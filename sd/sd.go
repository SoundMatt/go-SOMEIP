// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package sd implements SOME/IP Service Discovery (SOME/IP-SD).
//
// SOME/IP-SD is the mechanism by which ECUs announce their services
// (OfferService), locate remote services (FindService), and manage
// event group subscriptions (SubscribeEventgroup).
//
// All SD messages use the magic-cookie header:
//
//	ServiceID = 0xFFFF, MethodID = 0x8100
//
// The SD payload carries a Flags byte, a 3-byte reserved field, a
// 4-byte Entries Length, a variable-length Entries array, a 4-byte
// Options Length, and a variable-length Options array.
//
// This package provides:
//   - Wire encode/decode for SD entries and IPv4 endpoint options
//   - An in-process [Registry] for unit tests (no UDP sockets)
//
// Only the IPv4 unicast endpoint option (type 0x04) is implemented. IPv6
// endpoint options (type 0x06) and IPv4 multicast options (type 0x14) are not
// supported and their type constants are intentionally omitted rather than
// advertised without an encoder/decoder.
package sd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
)

// SOME/IP-SD magic cookie header fields.
const (
	SDServiceID someip.ServiceID = 0xFFFF
	SDMethodID  someip.MethodID  = 0x8100
)

// SD flags byte.
const (
	FlagReboot  uint8 = 0x80
	FlagUnicast uint8 = 0x40
)

// Entry type identifiers (AUTOSAR PRS_SOMEIPServiceDiscovery).
const (
	EntryTypeFind         uint8 = 0x00
	EntryTypeOffer        uint8 = 0x01
	EntryTypeSubscribe    uint8 = 0x06
	EntryTypeSubscribeAck uint8 = 0x07
)

// Option type identifiers. Only the IPv4 unicast endpoint option is
// encoded/decoded by this package (see EncodeIPv4Option/DecodeIPv4Option).
const (
	OptionTypeIPv4Endpoint uint8 = 0x04
)

// EntrySize is the fixed size of a SOME/IP-SD entry in bytes.
const EntrySize = 16

// ErrShortEntry is returned when a byte slice is too short for an SD entry.
var ErrShortEntry = errors.New("sd: byte slice too short for entry")

// ErrShortOption is returned when a byte slice is too short for an SD option.
var ErrShortOption = errors.New("sd: byte slice too short for option")

// ErrUnknownEntryType is returned for unrecognised entry type codes.
var ErrUnknownEntryType = errors.New("sd: unknown entry type")

// ── Entry types ───────────────────────────────────────────────────────────────

//fusa:req REQ-SD-001

// Entry represents a single SOME/IP-SD entry (16 bytes on the wire).
type Entry struct {
	// Type is EntryTypeFind, EntryTypeOffer, EntryTypeSubscribe, or EntryTypeSubscribeAck.
	Type uint8
	// Index1, Index2 are option run indices (0 when no options).
	Index1, Index2 uint8
	// NumOpts1, NumOpts2 are the number of options in each run.
	NumOpts1, NumOpts2 uint8
	// ServiceID identifies the service.
	ServiceID someip.ServiceID
	// InstanceID identifies the service instance. 0xFFFF = all instances.
	InstanceID someip.InstanceID
	// MajorVersion is the major interface version. 0xFF = any.
	MajorVersion uint8
	// TTL is the time-to-live in seconds. 0 = stop offering / unsubscribe.
	TTL uint32
	// MinorVersion (Offer/Find) or EventgroupID (Subscribe/SubscribeAck).
	// MinorVersion is a full 32-bit big-endian value at bytes 12–15 and is only
	// present on Offer/Find entries; Subscribe/SubscribeAck carry no MinorVersion
	// and instead use EventgroupID at bytes 14–15.
	MinorVersion uint32
	// EventgroupID is populated for Subscribe / SubscribeAck entries.
	EventgroupID uint16
}

// EncodeEntry serializes e into exactly [EntrySize] bytes appended to dst.
//
//fusa:req REQ-SD-002
func EncodeEntry(dst []byte, e Entry) []byte {
	b := [EntrySize]byte{}
	b[0] = e.Type
	// AUTOSAR PRS_SOMEIPServiceDiscoveryProtocol layout:
	//   byte1 = Index First Option Run (full 8 bits)
	//   byte2 = Index Second Option Run (full 8 bits)
	//   byte3 = high nibble Number of Options 1 | low nibble Number of Options 2
	b[1] = e.Index1
	b[2] = e.Index2
	// NumOpts1/NumOpts2 are 4-bit nibble fields; a count above 15 cannot be
	// represented on the wire. Left-shifting an unmasked NumOpts1 wraps
	// silently in the destination byte (e.g. 17 encodes indistinguishably
	// from 1), which would misrepresent how many options the entry's first
	// run actually covers. Saturate both fields at the field's maximum
	// instead so an over-large count always reads back as 15 (unambiguously
	// "too many to represent"), never as a smaller-but-plausible wrong count.
	numOpts1, numOpts2 := e.NumOpts1, e.NumOpts2
	if numOpts1 > 0x0F {
		numOpts1 = 0x0F
	}
	if numOpts2 > 0x0F {
		numOpts2 = 0x0F
	}
	b[3] = (numOpts1 << 4) | numOpts2
	binary.BigEndian.PutUint16(b[4:6], uint16(e.ServiceID))
	binary.BigEndian.PutUint16(b[6:8], uint16(e.InstanceID))
	b[8] = e.MajorVersion
	// TTL is 3 bytes (big-endian).
	b[9] = uint8(e.TTL >> 16)
	b[10] = uint8(e.TTL >> 8)
	b[11] = uint8(e.TTL)

	switch e.Type {
	case EntryTypeSubscribe, EntryTypeSubscribeAck:
		// Bytes 12–13 reserved (0x00), bytes 14–15 = EventgroupID.
		binary.BigEndian.PutUint16(b[14:16], e.EventgroupID)
	default:
		// Bytes 12–15 = MinorVersion (4 bytes, big-endian).
		binary.BigEndian.PutUint32(b[12:16], e.MinorVersion)
	}
	return append(dst, b[:]...)
}

// DecodeEntry parses an SD entry from the first [EntrySize] bytes of b.
//
//fusa:req REQ-SD-003
func DecodeEntry(b []byte) (Entry, error) {
	if len(b) < EntrySize {
		return Entry{}, fmt.Errorf("%w: got %d bytes, need %d", ErrShortEntry, len(b), EntrySize)
	}
	e := Entry{
		Type:         b[0],
		Index1:       b[1],
		Index2:       b[2],
		NumOpts1:     b[3] >> 4,
		NumOpts2:     b[3] & 0x0F,
		ServiceID:    someip.ServiceID(binary.BigEndian.Uint16(b[4:6])),
		InstanceID:   someip.InstanceID(binary.BigEndian.Uint16(b[6:8])),
		MajorVersion: b[8],
		TTL:          uint32(b[9])<<16 | uint32(b[10])<<8 | uint32(b[11]),
	}
	switch e.Type {
	case EntryTypeFind, EntryTypeOffer:
		e.MinorVersion = binary.BigEndian.Uint32(b[12:16])
	case EntryTypeSubscribe, EntryTypeSubscribeAck:
		e.EventgroupID = binary.BigEndian.Uint16(b[14:16])
	default:
		return Entry{}, fmt.Errorf("%w: 0x%02x", ErrUnknownEntryType, e.Type)
	}
	return e, nil
}

// ── IPv4 Endpoint option ──────────────────────────────────────────────────────

//fusa:req REQ-SD-004

// IPv4EndpointOption is a SOME/IP-SD IPv4 endpoint option (12 bytes on the wire).
type IPv4EndpointOption struct {
	// IP is the IPv4 address of the endpoint.
	IP net.IP
	// Protocol is 0x11 (UDP) or 0x06 (TCP).
	Protocol uint8
	// Port is the UDP or TCP port number.
	Port uint16
}

// ipv4OptionLength is the Length field value for IPv4 endpoint options (9 bytes after the header type).
const ipv4OptionLength = 9

// EncodeIPv4Option serializes opt into 12 bytes appended to dst.
func EncodeIPv4Option(dst []byte, optType uint8, opt IPv4EndpointOption) []byte {
	b := [12]byte{}
	// Length = 9 (Type byte through Port, not counting Length itself or the Type field of the header).
	binary.BigEndian.PutUint16(b[0:2], ipv4OptionLength)
	b[2] = optType
	b[3] = 0x00 // reserved
	ip := opt.IP.To4()
	if ip != nil {
		copy(b[4:8], ip)
	}
	b[8] = 0x00 // reserved
	b[9] = opt.Protocol
	binary.BigEndian.PutUint16(b[10:12], opt.Port)
	return append(dst, b[:]...)
}

// DecodeIPv4Option parses an IPv4 endpoint option from b.
// b must begin at the Length field (i.e. include the 3-byte common header).
func DecodeIPv4Option(b []byte) (IPv4EndpointOption, error) {
	if len(b) < 12 {
		return IPv4EndpointOption{}, fmt.Errorf("%w: got %d bytes, need 12", ErrShortOption, len(b))
	}
	ip := make(net.IP, 4)
	copy(ip, b[4:8])
	return IPv4EndpointOption{
		IP:       ip,
		Protocol: b[9],
		Port:     binary.BigEndian.Uint16(b[10:12]),
	}, nil
}

// Option is a generically-decoded SOME/IP-SD option: its wire Type and the
// full option bytes beginning at the Length field (the layout
// [DecodeIPv4Option] expects). Only [OptionTypeIPv4Endpoint] has an
// interpretable payload in this package; other types are preserved as raw
// bytes purely so that option-run indices into the array still line up.
type Option struct {
	Type uint8
	Raw  []byte
}

// DecodeOptions walks the SOME/IP-SD Options array in b and returns one
// [Option] per entry, in wire order. An [Entry]'s Index1/Index2 fields are
// indices into this slice (AUTOSAR PRS_SOMEIPServiceDiscoveryProtocol:
// "Index of First/Second Options Run"), not byte offsets, so callers must
// resolve option runs against the full returned slice.
//
//fusa:req REQ-SD-008
func DecodeOptions(b []byte) ([]Option, error) {
	var opts []Option
	offset := 0
	for offset < len(b) {
		if offset+3 > len(b) {
			return nil, fmt.Errorf("%w: truncated option header", ErrShortOption)
		}
		length := int(binary.BigEndian.Uint16(b[offset : offset+2]))
		optType := b[offset+2]
		// Physical size on the wire = 2 (Length field) + 1 (Type field) + length,
		// where Length counts every byte after Type (matches ipv4OptionLength=9
		// for the 12-byte IPv4 endpoint option: 2+1+9=12).
		size := 2 + 1 + length
		if length < 1 || offset+size > len(b) {
			return nil, fmt.Errorf("%w: option length %d exceeds remaining options bytes", ErrShortOption, length)
		}
		opts = append(opts, Option{Type: optType, Raw: b[offset : offset+size]})
		offset += size
	}
	return opts, nil
}

// ResolveIPv4Endpoint returns the IPv4 endpoint option referenced by entry's
// option runs (Index1/NumOpts1 and Index2/NumOpts2) in opts, decoding the
// first referenced option of type [OptionTypeIPv4Endpoint] it finds. ok is
// false if entry references no options, an option run's index/count falls
// outside opts (an out-of-range run is ignored rather than trusted — the
// index and count both come from the attacker-controlled entry), or none of
// the referenced options is a decodable IPv4 endpoint option.
//
//fusa:req REQ-SD-009
func ResolveIPv4Endpoint(entry Entry, opts []Option) (IPv4EndpointOption, bool) {
	runs := [2][2]uint8{{entry.Index1, entry.NumOpts1}, {entry.Index2, entry.NumOpts2}}
	for _, run := range runs {
		idx, num := int(run[0]), int(run[1])
		if num == 0 {
			continue
		}
		if idx < 0 || num < 0 || idx+num > len(opts) {
			continue
		}
		for _, opt := range opts[idx : idx+num] {
			if opt.Type != OptionTypeIPv4Endpoint {
				continue
			}
			if ep, err := DecodeIPv4Option(opt.Raw); err == nil {
				return ep, true
			}
		}
	}
	return IPv4EndpointOption{}, false
}

// ── In-process Registry (no UDP) ──────────────────────────────────────────────

// ServiceRecord holds a registered service offer.
type ServiceRecord struct {
	Entry    Entry
	Endpoint IPv4EndpointOption
	Expires  time.Time
}

// Registry is an in-process service registry for unit tests.
// It does not use any network sockets.
// A Registry is safe for concurrent use from multiple goroutines.
type Registry struct {
	mu       sync.RWMutex
	services map[registryKey]ServiceRecord
}

type registryKey struct {
	serviceID  someip.ServiceID
	instanceID someip.InstanceID
}

//fusa:req REQ-SD-005

// NewRegistry returns an empty in-process registry.
func NewRegistry() *Registry {
	return &Registry{services: make(map[registryKey]ServiceRecord)}
}

//fusa:req REQ-SD-006

// Offer registers a service offer. A zero TTL removes the entry.
func (r *Registry) Offer(entry Entry, endpoint IPv4EndpointOption) {
	key := registryKey{entry.ServiceID, entry.InstanceID}
	r.mu.Lock()
	if entry.TTL == 0 {
		delete(r.services, key)
	} else {
		r.services[key] = ServiceRecord{
			Entry:    entry,
			Endpoint: endpoint,
			Expires:  time.Now().Add(time.Duration(entry.TTL) * time.Second),
		}
	}
	r.mu.Unlock()
}

//fusa:req REQ-SD-007

// Find returns all non-expired service records matching serviceID.
// Use [someip.ServiceID](0xFFFF) to find all services.
func (r *Registry) Find(serviceID someip.ServiceID) []ServiceRecord {
	now := time.Now()
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []ServiceRecord
	for _, rec := range r.services {
		if rec.Expires.Before(now) {
			continue
		}
		if serviceID != 0xFFFF && rec.Entry.ServiceID != serviceID {
			continue
		}
		out = append(out, rec)
	}
	return out
}
