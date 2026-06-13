// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package e2e implements AUTOSAR End-to-End (E2E) communication protection.
//
// E2E protection detects data corruption and ensures message freshness across
// communication paths that are outside the control of the application.
// It prepends a small header containing a CRC and counter to each protected
// payload; the receiver verifies both before accepting the data.
//
// Two AUTOSAR-standardised profiles are provided:
//
//   - [Profile01]: CRC-8/SAE-J1850 with a 2-byte header. Suitable for short
//     PDUs (≤ 240 bytes) in safety-relevant low-bandwidth channels.
//
//   - [Profile05]: CRC-32/Ethernet with an 8-byte header. Suitable for larger
//     PDUs in high-throughput safety channels.
//
// # Usage
//
//	// Sender side
//	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x1234})
//	protected, _ := p.Protect(payload, counter)
//
//	// Receiver side
//	data, err := p.Check(protected, counter)
//	if err != nil {
//	    // data corrupt, out-of-sequence, or wrong DataID — do not use
//	}
package e2e

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrCRCMismatch is returned when the received CRC does not match the computed CRC.
var ErrCRCMismatch = errors.New("e2e: CRC mismatch")

// ErrCounterMismatch is returned when the received counter differs from the expected value.
var ErrCounterMismatch = errors.New("e2e: counter mismatch")

// ErrShortFrame is returned when the protected data is shorter than the E2E header.
var ErrShortFrame = errors.New("e2e: frame shorter than E2E header")

// ── CRC-8/SAE-J1850 (Profile 01) ─────────────────────────────────────────────

// crc8Table holds the pre-computed CRC-8/SAE-J1850 lookup table.
// Polynomial: 0x1D, initial value: 0xFF, XOR out: 0xFF, not reflected.
var crc8Table [256]uint8

func init() {
	for i := range crc8Table {
		crc := uint8(i)
		for j := 0; j < 8; j++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x1D
			} else {
				crc <<= 1
			}
		}
		crc8Table[i] = crc
	}
}

// crc8 computes the CRC-8/SAE-J1850 checksum over data, starting from init.
func crc8(init uint8, data []byte) uint8 {
	crc := init
	for _, b := range data {
		crc = crc8Table[crc^b]
	}
	return crc ^ 0xFF
}

// ── Profile 01 ────────────────────────────────────────────────────────────────

// Profile01HeaderSize is the number of bytes prepended by Profile 01.
const Profile01HeaderSize = 2

// Profile01Config configures a [Profile01] instance.
type Profile01Config struct {
	// DataID is a 16-bit identifier unique to the protected data element.
	// It is folded into the CRC to protect against message substitution.
	DataID uint16
}

// Profile01 implements AUTOSAR E2E Protection Profile 01.
//
// Header layout (2 bytes prepended to payload):
//
//	byte 0: CRC (CRC-8/SAE-J1850 over DataID bytes and payload)
//	byte 1: Counter[3:0] in low nibble | DataID[0] high nibble in high nibble
//
//fusa:req REQ-E2E-001
type Profile01 struct {
	cfg Profile01Config
}

// NewProfile01 creates a Profile01 instance for the given DataID.
//
//fusa:req REQ-E2E-002
func NewProfile01(cfg Profile01Config) *Profile01 {
	return &Profile01{cfg: cfg}
}

// Protect prepends the 2-byte E2E header to payload and returns the protected frame.
// counter must be in the range 0–14; value 15 (0xF) is reserved by AUTOSAR.
//
//fusa:req REQ-E2E-003
func (p *Profile01) Protect(payload []byte, counter uint8) ([]byte, error) {
	if counter > 14 {
		return nil, fmt.Errorf("e2e: Profile01 counter %d out of range [0,14]", counter)
	}

	// Header byte 1 = (DataID[0] & 0xF0) | (counter & 0x0F)
	hdr1 := (uint8(p.cfg.DataID) & 0xF0) | (counter & 0x0F)

	// CRC covers: DataID low byte, DataID high byte, hdr1, then payload bytes.
	crc := crc8(0xFF,
		append([]byte{uint8(p.cfg.DataID), uint8(p.cfg.DataID >> 8), hdr1}, payload...),
	)

	out := make([]byte, Profile01HeaderSize+len(payload))
	out[0] = crc
	out[1] = hdr1
	copy(out[Profile01HeaderSize:], payload)
	return out, nil
}

// Check verifies the E2E header in frame and returns the payload on success.
// It returns [ErrShortFrame], [ErrCRCMismatch], or [ErrCounterMismatch] on failure.
//
//fusa:req REQ-E2E-004
func (p *Profile01) Check(frame []byte, expectedCounter uint8) ([]byte, error) {
	if len(frame) < Profile01HeaderSize {
		return nil, fmt.Errorf("%w: got %d bytes, need at least %d", ErrShortFrame, len(frame), Profile01HeaderSize)
	}
	receivedCRC := frame[0]
	hdr1 := frame[1]
	counter := hdr1 & 0x0F
	payload := frame[Profile01HeaderSize:]

	// Recompute CRC over DataID bytes, hdr1, and payload.
	want := crc8(0xFF,
		append([]byte{uint8(p.cfg.DataID), uint8(p.cfg.DataID >> 8), hdr1}, payload...),
	)
	if receivedCRC != want {
		return nil, fmt.Errorf("%w: received 0x%02x, computed 0x%02x", ErrCRCMismatch, receivedCRC, want)
	}
	if counter != expectedCounter&0x0F {
		return nil, fmt.Errorf("%w: received %d, expected %d", ErrCounterMismatch, counter, expectedCounter&0x0F)
	}
	return payload, nil
}

// ── CRC-32/Ethernet (Profile 05) ─────────────────────────────────────────────

// crc32Table holds the pre-computed CRC-32/Ethernet (ISO-HDLC) lookup table.
// Polynomial: 0x04C11DB7 reflected = 0xEDB88320, init: 0xFFFFFFFF, XOR out: 0xFFFFFFFF.
var crc32Table [256]uint32

func init() {
	for i := range crc32Table {
		crc := uint32(i)
		for j := 0; j < 8; j++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
		crc32Table[i] = crc
	}
}

// crc32 computes the CRC-32/Ethernet checksum over data, starting from init.
func crc32(initVal uint32, data []byte) uint32 {
	crc := initVal
	for _, b := range data {
		crc = crc32Table[uint8(crc)^b] ^ (crc >> 8)
	}
	return crc ^ 0xFFFFFFFF
}

// Profile05HeaderSize is the number of bytes prepended by Profile 05.
const Profile05HeaderSize = 8

// Profile05Config configures a [Profile05] instance.
type Profile05Config struct {
	// DataID is an 8-bit identifier unique to the protected data element.
	// It is folded into the CRC to protect against message substitution.
	DataID uint8
}

// Profile05 implements AUTOSAR E2E Protection Profile 05.
//
// Header layout (8 bytes prepended to payload):
//
//	bytes 0-3: CRC-32/Ethernet (little-endian)
//	byte 4:    Counter (0–255, wraps)
//	bytes 5-7: Reserved (0x00)
//
//fusa:req REQ-E2E-005
type Profile05 struct {
	cfg Profile05Config
}

// NewProfile05 creates a Profile05 instance for the given DataID.
//
//fusa:req REQ-E2E-006
func NewProfile05(cfg Profile05Config) *Profile05 {
	return &Profile05{cfg: cfg}
}

// Protect prepends the 8-byte E2E header to payload and returns the protected frame.
//
//fusa:req REQ-E2E-007
func (p *Profile05) Protect(payload []byte, counter uint8) ([]byte, error) {
	out := make([]byte, Profile05HeaderSize+len(payload))
	out[4] = counter
	// bytes 5-7 are already zero (reserved)
	copy(out[Profile05HeaderSize:], payload)

	// CRC covers: DataID, counter, reserved bytes, then payload.
	// Header bytes 0-3 (CRC itself) are excluded from the computation.
	crcInput := append([]byte{p.cfg.DataID}, out[4:]...)
	checksum := crc32(0xFFFFFFFF, crcInput)
	binary.LittleEndian.PutUint32(out[0:4], checksum)
	return out, nil
}

// Check verifies the E2E header in frame and returns the payload on success.
// It returns [ErrShortFrame], [ErrCRCMismatch], or [ErrCounterMismatch] on failure.
//
//fusa:req REQ-E2E-008
func (p *Profile05) Check(frame []byte, expectedCounter uint8) ([]byte, error) {
	if len(frame) < Profile05HeaderSize {
		return nil, fmt.Errorf("%w: got %d bytes, need at least %d", ErrShortFrame, len(frame), Profile05HeaderSize)
	}
	receivedCRC := binary.LittleEndian.Uint32(frame[0:4])
	counter := frame[4]
	payload := frame[Profile05HeaderSize:]

	crcInput := append([]byte{p.cfg.DataID}, frame[4:]...)
	want := crc32(0xFFFFFFFF, crcInput)
	if receivedCRC != want {
		return nil, fmt.Errorf("%w: received 0x%08x, computed 0x%08x", ErrCRCMismatch, receivedCRC, want)
	}
	if counter != expectedCounter {
		return nil, fmt.Errorf("%w: received %d, expected %d", ErrCounterMismatch, counter, expectedCounter)
	}
	return payload, nil
}
