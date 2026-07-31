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
	0x00,       // Index1 (Index First Option Run)
	0x00,       // Index2 (Index Second Option Run)
	0x00,       // NumOpts1<<4 | NumOpts2
	0x12, 0x34, // ServiceID
	0x00, 0x01, // InstanceID
	0x01,             // MajorVersion
	0x00, 0x00, 0x03, // TTL = 3
	0x00, 0x00, 0x00, 0x00, // MinorVersion = 0
}

// TestEncodeEntryOptionRunLayout asserts the exact AUTOSAR PRS byte layout of
// the option-run fields (bytes 1–3): byte1 = Index1, byte2 = Index2 (full 8
// bits), byte3 = high nibble NumOpts1 | low nibble NumOpts2. This is a golden
// vector with Index2 > 15, NumOpts1 > 0 and NumOpts2 > 0 so the packing is
// actually exercised, cross-checked against the PRS rather than self round-trip.
func TestEncodeEntryOptionRunLayout(t *testing.T) {
	//fusa:test REQ-SD-002
	//fusa:test REQ-SD-003
	e := sd.Entry{
		Type:       sd.EntryTypeOffer,
		Index1:     0x01,
		Index2:     0x20, // > 15: must occupy the full byte2, never overflow byte2's nibble
		NumOpts1:   0x03,
		NumOpts2:   0x05,
		ServiceID:  0x1234,
		InstanceID: 0x0001,
		TTL:        3,
	}
	enc := sd.EncodeEntry(nil, e)
	if enc[1] != 0x01 {
		t.Errorf("byte1 (Index1): got 0x%02x, want 0x01", enc[1])
	}
	if enc[2] != 0x20 {
		t.Errorf("byte2 (Index2): got 0x%02x, want 0x20", enc[2])
	}
	if enc[3] != 0x35 { // (3<<4)|5
		t.Errorf("byte3 (NumOpts1<<4|NumOpts2): got 0x%02x, want 0x35", enc[3])
	}
	got, err := sd.DecodeEntry(enc)
	if err != nil {
		t.Fatalf("DecodeEntry: %v", err)
	}
	if got.Index1 != e.Index1 || got.Index2 != e.Index2 ||
		got.NumOpts1 != e.NumOpts1 || got.NumOpts2 != e.NumOpts2 {
		t.Errorf("option-run round-trip mismatch: got %+v, want Index1=%d Index2=%d NumOpts1=%d NumOpts2=%d",
			got, e.Index1, e.Index2, e.NumOpts1, e.NumOpts2)
	}
}

// TestEncodeEntryNumOptsSaturates is a regression test for go-SOMEIP-05:
// NumOpts1/NumOpts2 are 4-bit nibble fields, but EncodeEntry used to shift an
// unmasked NumOpts1 into byte3 unchecked, so an out-of-range count (e.g. 17)
// silently wrapped to an unrelated smaller value (1) instead of failing safe.
// EncodeEntry must instead saturate at the field's maximum (15) so an
// over-large count is always unambiguously "too many", never a
// smaller-but-plausible wrong count.
func TestEncodeEntryNumOptsSaturates(t *testing.T) {
	//fusa:test REQ-SD-002
	e := sd.Entry{
		Type:       sd.EntryTypeOffer,
		NumOpts1:   17, // > 0x0F: would wrap to nibble value 1 without saturation
		NumOpts2:   32, // > 0x0F: would wrap to nibble value 0 without saturation
		ServiceID:  0x1234,
		InstanceID: 0x0001,
	}
	enc := sd.EncodeEntry(nil, e)
	gotNumOpts1 := enc[3] >> 4
	gotNumOpts2 := enc[3] & 0x0F
	if gotNumOpts1 != 0x0F {
		t.Errorf("NumOpts1 nibble: got %d, want 15 (saturated), not a wrapped value", gotNumOpts1)
	}
	if gotNumOpts2 != 0x0F {
		t.Errorf("NumOpts2 nibble: got %d, want 15 (saturated), not a wrapped value", gotNumOpts2)
	}
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

// TestDecodeOptionsAndResolveIPv4Endpoint is a regression test for
// go-SOMEIP-03: an OfferService entry's real endpoint (IP/port/L4-protocol)
// is carried by the option it references via Index1/NumOpts1 into the
// decoded Options array, not implied by anything else. This verifies the
// options array decodes correctly and that ResolveIPv4Endpoint follows the
// entry's option-run indices to the right option.
func TestDecodeOptionsAndResolveIPv4Endpoint(t *testing.T) {
	//fusa:test REQ-SD-008
	//fusa:test REQ-SD-009
	want := sd.IPv4EndpointOption{
		IP:       net.ParseIP("192.168.1.50").To4(),
		Protocol: 0x06, // TCP — must not be silently reported as UDP
		Port:     30510,
	}
	optsBuf := sd.EncodeIPv4Option(nil, sd.OptionTypeIPv4Endpoint, want)

	opts, err := sd.DecodeOptions(optsBuf)
	if err != nil {
		t.Fatalf("DecodeOptions: %v", err)
	}
	if len(opts) != 1 {
		t.Fatalf("DecodeOptions: got %d options, want 1", len(opts))
	}
	if opts[0].Type != sd.OptionTypeIPv4Endpoint {
		t.Errorf("option Type: got 0x%02x, want 0x%02x", opts[0].Type, sd.OptionTypeIPv4Endpoint)
	}

	entry := sd.Entry{
		Type:     sd.EntryTypeOffer,
		Index1:   0,
		NumOpts1: 1,
	}
	got, ok := sd.ResolveIPv4Endpoint(entry, opts)
	if !ok {
		t.Fatal("ResolveIPv4Endpoint: ok = false, want true")
	}
	if !got.IP.Equal(want.IP) || got.Protocol != want.Protocol || got.Port != want.Port {
		t.Errorf("ResolveIPv4Endpoint: got %+v, want %+v", got, want)
	}
}

// TestResolveIPv4Endpoint_OutOfRangeRunIgnored is a regression test for
// go-SOMEIP-03: Index1/NumOpts1 come from an attacker-controlled SD entry.
// A run that falls outside the decoded Options array must be ignored, not
// trusted (which would panic or read unrelated memory via a bad slice).
func TestResolveIPv4Endpoint_OutOfRangeRunIgnored(t *testing.T) {
	//fusa:test REQ-SD-009
	opts := []sd.Option{} // no options decoded at all
	entry := sd.Entry{
		Type:     sd.EntryTypeOffer,
		Index1:   5,
		NumOpts1: 3, // references options[5:8] into an empty slice
	}
	_, ok := sd.ResolveIPv4Endpoint(entry, opts)
	if ok {
		t.Error("ResolveIPv4Endpoint: ok = true for an out-of-range option run, want false")
	}
}

// TestDecodeOptions_TruncatedHeaderIsMalformed verifies a truncated option
// header is rejected rather than read out of bounds.
func TestDecodeOptions_TruncatedHeaderIsMalformed(t *testing.T) {
	//fusa:test REQ-SD-008
	_, err := sd.DecodeOptions([]byte{0x00, 0x09}) // only 2 of the required 3 header bytes
	if !errors.Is(err, sd.ErrShortOption) {
		t.Errorf("DecodeOptions: err = %v, want ErrShortOption", err)
	}
}

// TestDecodeOptions_LengthExceedsBufferIsMalformed verifies a Length field
// claiming more bytes than are actually present is rejected, not used to
// read (or slice) past the end of the buffer.
func TestDecodeOptions_LengthExceedsBufferIsMalformed(t *testing.T) {
	//fusa:test REQ-SD-008
	b := []byte{0xFF, 0xFF, 0x04} // Length=65535, Type=IPv4Endpoint, no data follows
	_, err := sd.DecodeOptions(b)
	if !errors.Is(err, sd.ErrShortOption) {
		t.Errorf("DecodeOptions: err = %v, want ErrShortOption", err)
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
