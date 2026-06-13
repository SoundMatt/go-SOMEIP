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
	EntryTypeFind             uint8 = 0x00
	EntryTypeOffer            uint8 = 0x01
	EntryTypeSubscribe        uint8 = 0x06
	EntryTypeSubscribeAck     uint8 = 0x07
)

// Option type identifiers.
const (
	OptionTypeIPv4Endpoint  uint8 = 0x04
	OptionTypeIPv6Endpoint  uint8 = 0x06
	OptionTypeIPv4Multicast uint8 = 0x14
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
	// Stored in the lower 24 bits on the wire for Offer/Find, lower 16 bits for Subscribe.
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
	b[1] = e.Index1
	b[2] = (e.NumOpts1 & 0x0F) | (e.Index2 << 4)
	b[3] = e.NumOpts2 & 0x0F
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
		Index2:       b[2] >> 4,
		NumOpts1:     b[2] & 0x0F,
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

// optionHeaderSize is the 3-byte common option header (Length[2] + Type[1]).
const optionHeaderSize = 3

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

// NewRegistry returns an empty in-process registry.
func NewRegistry() *Registry {
	return &Registry{services: make(map[registryKey]ServiceRecord)}
}

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
