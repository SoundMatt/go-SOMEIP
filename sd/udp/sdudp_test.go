// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package sdudp

import (
	"net"
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/sd"
)

// ── Payload codec unit tests (no network, no build tag) ───────────────────────

func TestEncodeDecodeSDFrame_OfferNoOption(t *testing.T) {
	//fusa:test REQ-SDUDP-001
	//fusa:test REQ-SDUDP-002
	entry := sd.Entry{
		Type:         sd.EntryTypeOffer,
		ServiceID:    someip.ServiceID(0x1234),
		InstanceID:   someip.InstanceID(0x0001),
		MajorVersion: 0x01,
		TTL:          3,
		MinorVersion: 0x00000001,
	}
	frame := encodeSDFrame(nil, sd.FlagReboot, entry, nil, 0)
	if len(frame) == 0 {
		t.Fatal("empty frame")
	}

	// Re-parse at the SD payload level.
	// Frame starts with a 16-byte SOME/IP header; payload follows.
	if len(frame) < 16 {
		t.Fatalf("frame too short: %d bytes", len(frame))
	}
	payload := frame[16:]
	msg, err := decodeSDPayload(payload)
	if err != nil {
		t.Fatalf("decodeSDPayload: %v", err)
	}
	if len(msg.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(msg.Entries))
	}
	got := msg.Entries[0]
	if got.ServiceID != entry.ServiceID {
		t.Errorf("ServiceID = 0x%04x, want 0x%04x", got.ServiceID, entry.ServiceID)
	}
	if got.InstanceID != entry.InstanceID {
		t.Errorf("InstanceID = 0x%04x, want 0x%04x", got.InstanceID, entry.InstanceID)
	}
	if got.TTL != entry.TTL {
		t.Errorf("TTL = %d, want %d", got.TTL, entry.TTL)
	}
}

func TestEncodeDecodeSDFrame_OfferWithEndpointOption(t *testing.T) {
	//fusa:test REQ-SDUDP-001
	//fusa:test REQ-SDUDP-002
	entry := sd.Entry{
		Type:         sd.EntryTypeOffer,
		ServiceID:    someip.ServiceID(0x5678),
		InstanceID:   someip.InstanceID(0x0002),
		MajorVersion: 0x02,
		TTL:          10,
		MinorVersion: 0x00000002,
	}
	opt := sd.IPv4EndpointOption{
		IP:       net.ParseIP("192.168.1.10"),
		Protocol: 0x11, // UDP
		Port:     30509,
	}
	frame := encodeSDFrame(nil, sd.FlagReboot, entry, &opt, sd.OptionTypeIPv4Endpoint)
	if len(frame) == 0 {
		t.Fatal("empty frame")
	}
	payload := frame[16:]
	msg, err := decodeSDPayload(payload)
	if err != nil {
		t.Fatalf("decodeSDPayload: %v", err)
	}
	if len(msg.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(msg.Entries))
	}
	got := msg.Entries[0]
	// With an option, NumOpts1 should be set.
	if got.NumOpts1 != 1 {
		t.Errorf("NumOpts1 = %d, want 1", got.NumOpts1)
	}
}

func TestEncodeDecodeSDFrame_Find(t *testing.T) {
	//fusa:test REQ-SDUDP-001
	//fusa:test REQ-SDUDP-002
	//fusa:test REQ-SDUDP-007
	entry := sd.Entry{
		Type:         sd.EntryTypeFind,
		ServiceID:    someip.ServiceID(0xAAAA),
		InstanceID:   0xFFFF,
		MajorVersion: 0xFF,
		TTL:          DefaultTTL,
		MinorVersion: 0xFFFFFFFF,
	}
	frame := encodeSDFrame(nil, sd.FlagUnicast, entry, nil, 0)
	payload := frame[16:]
	msg, err := decodeSDPayload(payload)
	if err != nil {
		t.Fatalf("decodeSDPayload: %v", err)
	}
	if len(msg.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(msg.Entries))
	}
	if msg.Entries[0].Type != sd.EntryTypeFind {
		t.Errorf("Type = 0x%02x, want EntryTypeFind", msg.Entries[0].Type)
	}
}

func TestEncodeDecodeSDFrame_Subscribe(t *testing.T) {
	//fusa:test REQ-SDUDP-001
	//fusa:test REQ-SDUDP-009
	entry := sd.Entry{
		Type:         sd.EntryTypeSubscribe,
		ServiceID:    someip.ServiceID(0x1234),
		InstanceID:   someip.InstanceID(0x0001),
		MajorVersion: 0x01,
		TTL:          DefaultTTL,
		EventgroupID: 0x0001,
	}
	frame := encodeSDFrame(nil, 0x00, entry, nil, 0)
	payload := frame[16:]
	msg, err := decodeSDPayload(payload)
	if err != nil {
		t.Fatalf("decodeSDPayload: %v", err)
	}
	if len(msg.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(msg.Entries))
	}
	if msg.Entries[0].EventgroupID != entry.EventgroupID {
		t.Errorf("EventgroupID = %d, want %d", msg.Entries[0].EventgroupID, entry.EventgroupID)
	}
}

func TestDecodeSDPayload_TooShort(t *testing.T) {
	//fusa:test REQ-SDUDP-002
	_, err := decodeSDPayload([]byte{0x80, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for truncated payload, got nil")
	}
}

func TestDecodeSDPayload_EntriesLenOverflow(t *testing.T) {
	//fusa:test REQ-SDUDP-002
	// Flags(4) + EntriesLen(4 = 0xFFFFFFFF) — entries extend past end of slice.
	payload := make([]byte, 8)
	payload[4] = 0xFF
	payload[5] = 0xFF
	payload[6] = 0xFF
	payload[7] = 0xFF
	_, err := decodeSDPayload(payload)
	if err == nil {
		t.Error("expected error for overflow entries length, got nil")
	}
}

func TestEncodeDecodeSDFrame_StopOffer(t *testing.T) {
	//fusa:test REQ-SDUDP-001
	//fusa:test REQ-SDUDP-008
	entry := sd.Entry{
		Type:         sd.EntryTypeOffer,
		ServiceID:    someip.ServiceID(0x1234),
		InstanceID:   someip.InstanceID(0x0001),
		MajorVersion: 0x01,
		TTL:          0, // stop-offer
	}
	frame := encodeSDFrame(nil, sd.FlagReboot, entry, nil, 0)
	payload := frame[16:]
	msg, err := decodeSDPayload(payload)
	if err != nil {
		t.Fatalf("decodeSDPayload: %v", err)
	}
	if msg.Entries[0].TTL != 0 {
		t.Errorf("TTL = %d, want 0 for stop-offer", msg.Entries[0].TTL)
	}
}

func TestEncodeSDFrame_AppendsToDst(t *testing.T) {
	//fusa:test REQ-SDUDP-001
	header := []byte("HEADER")
	entry := sd.Entry{Type: sd.EntryTypeFind, ServiceID: 0x0001, InstanceID: 0xFFFF, MajorVersion: 0xFF, TTL: 1, MinorVersion: 0xFFFFFFFF}
	result := encodeSDFrame(header, 0x00, entry, nil, 0)
	if string(result[:6]) != "HEADER" {
		t.Error("dst prefix was not preserved")
	}
}
