// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package tp implements SOME/IP Transport Protocol (SOME/IP-TP) segmentation
// and reassembly for payloads that exceed a single UDP or TCP segment.
//
// SOME/IP-TP splits large application payloads into fixed-size segments.
// Each segment carries:
//   - A SOME/IP header where bit 5 of MessageType is set (0x20 offset)
//   - A 4-byte TP header prepended to the payload: Offset (28 bits) | More (1 bit)
//
// The Offset field counts the byte position of this segment's payload within
// the complete reassembled payload, in units of 16 bytes.
//
// # Segmenting
//
// [Segment] splits a [someip.Message] into a slice of TP-framed messages.
// Each segment's payload is at most [DefaultSegmentSize] bytes.
//
// # Reassembling
//
// [Reassembler] collects TP segments keyed by (ServiceID, MethodID, SessionID)
// and emits the complete message when all segments have arrived. Incomplete
// assemblies are evicted after a configurable timeout.
package tp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
)

// DefaultSegmentSize is the maximum payload bytes per TP segment.
// Sized to fit within a typical Ethernet frame with SOME/IP + TP headers.
const DefaultSegmentSize = 1392

// DefaultReassemblyTimeout is the maximum time to wait for all segments of
// a TP message before discarding partially received data.
const DefaultReassemblyTimeout = 5 * time.Second

// tpBit is the TP flag in MessageType (bit 5).
const tpBit MessageType = 0x20

// MessageType mirrors someip.MessageType so we can manipulate bit 5 without
// importing cycle issues (tp imports someip; the tp bit is local).
type MessageType = someip.MessageType

// TP header size: 4 bytes (Offset[28 bits] | More[1 bit] | Reserved[3 bits]).
const tpHeaderSize = 4

// ErrSegmentTooLarge is returned when the caller requests a segment size < 16.
var ErrSegmentTooLarge = errors.New("tp: segment size must be at least 16 bytes")

// ErrMalformedSegment is returned when a received TP frame cannot be parsed.
var ErrMalformedSegment = errors.New("tp: malformed TP segment")

// ErrReassemblyTimeout is returned by [Reassembler.Add] when the assembly
// window for a message has expired.
var ErrReassemblyTimeout = errors.New("tp: reassembly timeout")

// ── Segmentation ─────────────────────────────────────────────────────────────

// Segment splits msg into one or more TP-framed messages.
// If msg.Payload fits within segmentSize, a single non-TP message is returned
// (the TP overhead is not needed).
// segmentSize is the maximum application-payload bytes per segment; if 0,
// [DefaultSegmentSize] is used.
//
//fusa:req REQ-TP-001
func Segment(msg someip.Message, segmentSize int) ([]someip.Message, error) {
	if segmentSize == 0 {
		segmentSize = DefaultSegmentSize
	}
	if segmentSize < 16 {
		return nil, ErrSegmentTooLarge
	}

	payload := msg.Payload
	if len(payload) <= segmentSize {
		return []someip.Message{msg}, nil
	}

	// TP offsets are in units of 16 bytes.
	if segmentSize%16 != 0 {
		segmentSize = (segmentSize / 16) * 16
	}

	var segments []someip.Message
	offset := 0
	for offset < len(payload) {
		end := offset + segmentSize
		more := true
		if end >= len(payload) {
			end = len(payload)
			more = false
		}

		chunk := payload[offset:end]
		// TP header: Offset in 16-byte units (upper 28 bits) | More flag (bit 3 of byte 3).
		offsetUnits := uint32(offset / 16)
		tpHdr := make([]byte, tpHeaderSize)
		binary.BigEndian.PutUint32(tpHdr, offsetUnits<<4)
		if more {
			tpHdr[3] |= 0x01
		}

		segPayload := make([]byte, tpHeaderSize+len(chunk))
		copy(segPayload, tpHdr)
		copy(segPayload[tpHeaderSize:], chunk)

		seg := someip.Message{
			ServiceID:        msg.ServiceID,
			MethodID:         msg.MethodID,
			ClientID:         msg.ClientID,
			SessionID:        msg.SessionID,
			ProtocolVersion:  msg.ProtocolVersion,
			InterfaceVersion: msg.InterfaceVersion,
			MessageType:      msg.MessageType | tpBit,
			ReturnCode:       msg.ReturnCode,
			Payload:          segPayload,
		}
		segments = append(segments, seg)
		offset = end
	}
	return segments, nil
}

// IsTP reports whether msg carries SOME/IP-TP framing (bit 5 of MessageType set).
func IsTP(msg someip.Message) bool {
	return msg.MessageType&tpBit != 0
}

// BaseMessageType strips the TP bit from MessageType.
func BaseMessageType(mt someip.MessageType) someip.MessageType {
	return mt &^ tpBit
}

// ── Reassembly ────────────────────────────────────────────────────────────────

type assemblyKey struct {
	serviceID someip.ServiceID
	methodID  someip.MethodID
	sessionID someip.SessionID
}

type assemblyWindow struct {
	// template holds header fields copied from the first segment.
	template someip.Message
	// segments maps byte-offset → payload chunk.
	segments map[int][]byte
	// totalSize is the total payload length, known once the last segment arrives.
	totalSize int
	// received tracks how many bytes have been received so far.
	received int
	expires  time.Time
}

// ReassemblerConfig configures a [Reassembler].
type ReassemblerConfig struct {
	// Timeout is the maximum age of an incomplete assembly window.
	// Zero uses [DefaultReassemblyTimeout].
	Timeout time.Duration
	// GCInterval is how often expired windows are evicted.
	// Zero defaults to Timeout/2.
	GCInterval time.Duration
}

// Reassembler collects TP segments and emits complete messages.
// A Reassembler is safe for concurrent use from multiple goroutines.
//
//fusa:req REQ-TP-002
type Reassembler struct {
	timeout time.Duration
	mu      sync.Mutex
	windows map[assemblyKey]*assemblyWindow
	ticker  *time.Ticker
	done    chan struct{}
}

// NewReassembler creates a [Reassembler] that evicts stale windows periodically.
//
//fusa:req REQ-TP-003
func NewReassembler(cfg ReassemblerConfig) *Reassembler {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultReassemblyTimeout
	}
	gcInterval := cfg.GCInterval
	if gcInterval == 0 {
		gcInterval = timeout / 2
	}
	r := &Reassembler{
		timeout: timeout,
		windows: make(map[assemblyKey]*assemblyWindow),
		ticker:  time.NewTicker(gcInterval),
		done:    make(chan struct{}),
	}
	go r.gcLoop()
	return r
}

// Add processes one TP segment and returns the reassembled message when
// all segments for a (ServiceID, MethodID, SessionID) tuple have arrived.
// Returns nil, nil when more segments are still expected.
// Returns nil, [ErrReassemblyTimeout] if the assembly window has expired.
// Returns nil, [ErrMalformedSegment] if the segment TP header is malformed.
//
//fusa:req REQ-TP-004
func (r *Reassembler) Add(seg someip.Message) (*someip.Message, error) {
	if !IsTP(seg) {
		// Non-TP message: pass through directly.
		return &seg, nil
	}
	if len(seg.Payload) < tpHeaderSize {
		return nil, fmt.Errorf("%w: payload shorter than TP header", ErrMalformedSegment)
	}

	tpWord := binary.BigEndian.Uint32(seg.Payload[:tpHeaderSize])
	offsetUnits := tpWord >> 4
	more := (tpWord & 0x01) != 0
	byteOffset := int(offsetUnits) * 16
	chunk := seg.Payload[tpHeaderSize:]

	key := assemblyKey{seg.ServiceID, seg.MethodID, seg.SessionID}

	r.mu.Lock()
	defer r.mu.Unlock()

	win, exists := r.windows[key]
	if !exists {
		win = &assemblyWindow{
			template:  seg,
			segments:  make(map[int][]byte),
			totalSize: -1,
			expires:   time.Now().Add(r.timeout),
		}
		r.windows[key] = win
	} else if time.Now().After(win.expires) {
		delete(r.windows, key)
		return nil, ErrReassemblyTimeout
	}

	// Store the chunk if not already received.
	if _, dup := win.segments[byteOffset]; !dup {
		buf := make([]byte, len(chunk))
		copy(buf, chunk)
		win.segments[byteOffset] = buf
		win.received += len(chunk)
	}

	if !more {
		win.totalSize = byteOffset + len(chunk)
	}

	// Check if all bytes have been received.
	if win.totalSize < 0 || win.received < win.totalSize {
		return nil, nil
	}

	// Reassemble.
	payload := make([]byte, win.totalSize)
	for off, data := range win.segments {
		copy(payload[off:], data)
	}
	delete(r.windows, key)

	msg := win.template
	msg.MessageType = BaseMessageType(win.template.MessageType)
	msg.Payload = payload
	return &msg, nil
}

// Close stops the GC goroutine and discards all pending assembly windows.
//
//fusa:req REQ-TP-005
func (r *Reassembler) Close() {
	r.ticker.Stop()
	close(r.done)
	r.mu.Lock()
	r.windows = make(map[assemblyKey]*assemblyWindow)
	r.mu.Unlock()
}

func (r *Reassembler) gcLoop() {
	for {
		select {
		case <-r.ticker.C:
			now := time.Now()
			r.mu.Lock()
			for key, win := range r.windows {
				if now.After(win.expires) {
					delete(r.windows, key)
				}
			}
			r.mu.Unlock()
		case <-r.done:
			return
		}
	}
}
