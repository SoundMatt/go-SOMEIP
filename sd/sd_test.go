// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package sd_test

import (
	"errors"
	"net"
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/sd"
)

// knownGoodOfferEntry is a hand-crafted OfferService entry.
// ServiceID=0x1234, InstanceID=0x0001, MajorVersion=0x01, TTL=3, MinorVersion=0.
var knownGoodOfferEntry = []byte{
	0x01,       // Type = OfferService
	0x00,       // Index1
	0x00,       // NumOpts1/Index2
	0x00,       // NumOpts2
	0x12, 0x34, // ServiceID
	0x00, 0x01, // InstanceID
	0x01,             // MajorVersion
	0x00, 0x00, 0x03, // TTL = 3
	0x00, 0x00, 0x00, 0x00, // MinorVersion = 0
}

func TestDecodeOfferEntry(t *testing.T) {
	//fusa:test REQ-SD-003
	e, err := sd.DecodeEntry(knownGoodOfferEntry)
	if err != nil {
		t.Fatalf("DecodeEntry: %v", err)
	}
	if e.Type != sd.EntryTypeOffer {
		t.Errorf("Type: got 0x%02x, want Offer", e.Type)
	}
	if e.ServiceID != 0x1234 {
		t.Errorf("ServiceID: got 0x%04x, want 0x1234", e.ServiceID)
	}
	if e.InstanceID != 0x0001 {
		t.Errorf("InstanceID: got 0x%04x, want 0x0001", e.InstanceID)
	}
	if e.TTL != 3 {
		t.Errorf("TTL: got %d, want 3", e.TTL)
	}
}

func TestEncodeDecodeEntryRoundTrip(t *testing.T) {
	//fusa:test REQ-SD-001
	//fusa:test REQ-SD-002
	//fusa:test REQ-SD-003
	original := sd.Entry{
		Type:         sd.EntryTypeOffer,
		ServiceID:    0xABCD,
		InstanceID:   0x0042,
		MajorVersion: 0x02,
		TTL:          0x00FFFF,
		MinorVersion: 0x00000001,
	}
	enc := sd.EncodeEntry(nil, original)
	if len(enc) != sd.EntrySize {
		t.Fatalf("encoded length: got %d, want %d", len(enc), sd.EntrySize)
	}
	got, err := sd.DecodeEntry(enc)
	if err != nil {
		t.Fatalf("DecodeEntry: %v", err)
	}
	if got.ServiceID != original.ServiceID {
		t.Errorf("ServiceID mismatch")
	}
	if got.TTL != original.TTL {
		t.Errorf("TTL mismatch: got %d want %d", got.TTL, original.TTL)
	}
	if got.MinorVersion != original.MinorVersion {
		t.Errorf("MinorVersion mismatch")
	}
}

func TestEncodeDecodeSubscribeEntry(t *testing.T) {
	//fusa:test REQ-SD-002
	//fusa:test REQ-SD-003
	original := sd.Entry{
		Type:         sd.EntryTypeSubscribe,
		ServiceID:    0x1234,
		InstanceID:   0x0001,
		MajorVersion: 0x01,
		TTL:          5,
		EventgroupID: 0x0010,
	}
	enc := sd.EncodeEntry(nil, original)
	got, err := sd.DecodeEntry(enc)
	if err != nil {
		t.Fatalf("DecodeEntry subscribe: %v", err)
	}
	if got.EventgroupID != original.EventgroupID {
		t.Errorf("EventgroupID: got 0x%04x, want 0x%04x", got.EventgroupID, original.EventgroupID)
	}
}

func TestDecodeShortEntry(t *testing.T) {
	//fusa:test REQ-SD-003
	_, err := sd.DecodeEntry([]byte{0x01, 0x00})
	if !errors.Is(err, sd.ErrShortEntry) {
		t.Errorf("expected ErrShortEntry, got %v", err)
	}
}

func TestDecodeUnknownEntryType(t *testing.T) {
	//fusa:test REQ-SD-003
	b := make([]byte, sd.EntrySize)
	b[0] = 0xFF // unknown type
	_, err := sd.DecodeEntry(b)
	if !errors.Is(err, sd.ErrUnknownEntryType) {
		t.Errorf("expected ErrUnknownEntryType, got %v", err)
	}
}

func TestIPv4OptionRoundTrip(t *testing.T) {
	//fusa:test REQ-SD-004
	original := sd.IPv4EndpointOption{
		IP:       net.ParseIP("192.168.1.100").To4(),
		Protocol: 0x11, // UDP
		Port:     30509,
	}
	enc := sd.EncodeIPv4Option(nil, sd.OptionTypeIPv4Endpoint, original)
	if len(enc) != 12 {
		t.Fatalf("IPv4 option encoded length: got %d, want 12", len(enc))
	}
	got, err := sd.DecodeIPv4Option(enc)
	if err != nil {
		t.Fatalf("DecodeIPv4Option: %v", err)
	}
	if !got.IP.Equal(original.IP) {
		t.Errorf("IP: got %v, want %v", got.IP, original.IP)
	}
	if got.Protocol != original.Protocol {
		t.Errorf("Protocol: got 0x%02x, want 0x%02x", got.Protocol, original.Protocol)
	}
	if got.Port != original.Port {
		t.Errorf("Port: got %d, want %d", got.Port, original.Port)
	}
}

func TestRegistryOfferAndFind(t *testing.T) {
	//fusa:test REQ-SD-005
	//fusa:test REQ-SD-006
	//fusa:test REQ-SD-007
	reg := sd.NewRegistry()

	entry := sd.Entry{
		Type:         sd.EntryTypeOffer,
		ServiceID:    someip.ServiceID(0x1234),
		InstanceID:   someip.InstanceID(0x0001),
		MajorVersion: 0x01,
		TTL:          60,
	}
	endpoint := sd.IPv4EndpointOption{
		IP:       net.ParseIP("10.0.0.1").To4(),
		Protocol: 0x11,
		Port:     30509,
	}
	reg.Offer(entry, endpoint)

	records := reg.Find(someip.ServiceID(0x1234))
	if len(records) != 1 {
		t.Fatalf("Find: got %d records, want 1", len(records))
	}
	if records[0].Entry.ServiceID != 0x1234 {
		t.Errorf("record ServiceID mismatch")
	}
}

func TestRegistryFindAll(t *testing.T) {
	//fusa:test REQ-SD-007
	reg := sd.NewRegistry()
	for i := someip.ServiceID(0x0001); i <= 0x0003; i++ {
		reg.Offer(sd.Entry{
			Type:       sd.EntryTypeOffer,
			ServiceID:  i,
			InstanceID: 0x0001,
			TTL:        60,
		}, sd.IPv4EndpointOption{})
	}
	all := reg.Find(0xFFFF)
	if len(all) != 3 {
		t.Errorf("Find(0xFFFF): got %d records, want 3", len(all))
	}
}

func TestRegistryOfferTTLZeroRemoves(t *testing.T) {
	//fusa:test REQ-SD-006
	reg := sd.NewRegistry()
	entry := sd.Entry{
		Type: sd.EntryTypeOffer, ServiceID: 0x1234, InstanceID: 0x0001, TTL: 60,
	}
	reg.Offer(entry, sd.IPv4EndpointOption{})
	entry.TTL = 0
	reg.Offer(entry, sd.IPv4EndpointOption{})

	records := reg.Find(0x1234)
	if len(records) != 0 {
		t.Errorf("after TTL=0 Offer: expected 0 records, got %d", len(records))
	}
}

func TestSDMagicCookieConstants(t *testing.T) {
	if sd.SDServiceID != 0xFFFF {
		t.Errorf("SDServiceID: got 0x%04x, want 0xFFFF", sd.SDServiceID)
	}
	if sd.SDMethodID != 0x8100 {
		t.Errorf("SDMethodID: got 0x%04x, want 0x8100", sd.SDMethodID)
	}
}
