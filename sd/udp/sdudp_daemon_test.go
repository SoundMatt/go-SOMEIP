// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package sdudp

import (
	"net"
	"testing"

	"github.com/SoundMatt/go-SOMEIP/sd"
)

// These whitebox tests exercise Daemon lifecycle plumbing (NewDaemon,
// Registry, Offer) using a real — but ephemeral-port — multicast socket, so
// they run without the `integration` build tag: no cross-process delivery is
// required (unlike the full offer/find/subscribe round trip a real deployment
// would exercise), so there is nothing network-flaky to depend on. A ":0"
// port asks the OS for an unused ephemeral port, avoiding collisions with
// other tests or a real SD daemon bound to the fixed 30490 port.

func TestNewDaemonRegistry(t *testing.T) {
	//fusa:test REQ-SDUDP-003
	//fusa:test REQ-SDUDP-004
	//fusa:test REQ-SDUDP-005
	d, err := NewDaemon(DaemonConfig{MulticastAddr: "239.192.255.251:0"})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	defer func() { _ = d.Close() }()

	reg := d.Registry()
	if reg == nil {
		t.Fatal("Registry() returned nil")
	}
	if got := reg.Find(0xFFFF); len(got) != 0 {
		t.Errorf("Registry: got %d records, want 0 for a fresh daemon", len(got))
	}
}

func TestDaemonOfferRegistersOffer(t *testing.T) {
	//fusa:test REQ-SDUDP-006
	d, err := NewDaemon(DaemonConfig{MulticastAddr: "239.192.255.251:0"})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	defer func() { _ = d.Close() }()

	cfg := OfferConfig{
		Entry:    sd.Entry{ServiceID: 0x1234, InstanceID: 0x0001, MajorVersion: 0x01},
		Endpoint: sd.IPv4EndpointOption{IP: net.ParseIP("127.0.0.1"), Protocol: 0x11, Port: 30509},
	}
	d.Offer(cfg)

	key := offerKey{serviceID: 0x1234, instanceID: 0x0001}
	d.mu.RLock()
	rec, ok := d.offers[key]
	d.mu.RUnlock()
	if !ok {
		t.Fatal("Offer did not register an offerRecord for the given service/instance")
	}
	if rec.cfg.Entry.Type != sd.EntryTypeOffer {
		t.Errorf("offerRecord.Entry.Type: got 0x%02x, want EntryTypeOffer", rec.cfg.Entry.Type)
	}
	if rec.cfg.Entry.TTL != DefaultTTL {
		t.Errorf("offerRecord.Entry.TTL: got %d, want default %d", rec.cfg.Entry.TTL, DefaultTTL)
	}

	// Offer again with the same key: per its documented behaviour this
	// replaces (not duplicates) the existing offer.
	d.Offer(cfg)
	d.mu.RLock()
	_, stillThere := d.offers[key]
	n := len(d.offers)
	d.mu.RUnlock()
	if !stillThere {
		t.Fatal("re-Offer removed the entry instead of replacing it")
	}
	if n != 1 {
		t.Errorf("offers map size after re-Offer: got %d, want 1 (replace, not append)", n)
	}
}

// TestHandleEntryOffer_UsesReferencedEndpointOption is a regression test for
// go-SOMEIP-03: an OfferService entry's registered endpoint must come from
// the IPv4 endpoint option it references (Index1/NumOpts1), not the UDP
// source address/port the SD packet happened to arrive on — those are
// typically different (SD multicast source port 30490 vs. the service's own
// port), and a TCP-offered service must not be registered as UDP.
func TestHandleEntryOffer_UsesReferencedEndpointOption(t *testing.T) {
	//fusa:test REQ-SDUDP-002
	d, err := NewDaemon(DaemonConfig{MulticastAddr: "239.192.255.251:0"})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	defer func() { _ = d.Close() }()

	entry := sd.Entry{
		Type:       sd.EntryTypeOffer,
		ServiceID:  0x1234,
		InstanceID: 0x0001,
		TTL:        60,
		Index1:     0,
		NumOpts1:   1,
	}
	realEndpoint := sd.IPv4EndpointOption{
		IP:       net.ParseIP("10.0.0.42").To4(),
		Protocol: 0x06, // TCP — the SD packet still arrives over UDP
		Port:     30777,
	}
	opts := []sd.Option{{
		Type: sd.OptionTypeIPv4Endpoint,
		Raw:  sd.EncodeIPv4Option(nil, sd.OptionTypeIPv4Endpoint, realEndpoint),
	}}
	// The SD packet's source address/port, which must NOT end up in the
	// registry now that the entry references a real endpoint option.
	from := &net.UDPAddr{IP: net.ParseIP("192.168.99.99"), Port: 30490}

	d.handleEntry(entry, opts, from)

	records := d.Registry().Find(0x1234)
	if len(records) != 1 {
		t.Fatalf("Registry.Find: got %d records, want 1", len(records))
	}
	got := records[0].Endpoint
	if !got.IP.Equal(realEndpoint.IP) {
		t.Errorf("registered IP = %v, want %v (the option's IP, not the SD source %v)", got.IP, realEndpoint.IP, from.IP)
	}
	if got.Protocol != realEndpoint.Protocol {
		t.Errorf("registered Protocol = 0x%02x, want 0x%02x (TCP, not hardcoded UDP)", got.Protocol, realEndpoint.Protocol)
	}
	if got.Port != realEndpoint.Port {
		t.Errorf("registered Port = %d, want %d (the option's port, not the SD source port %d)", got.Port, realEndpoint.Port, from.Port)
	}
}

// TestHandleEntryOffer_FallsBackToSourceWhenNoOption verifies that an offer
// with no resolvable endpoint option still registers something (best-effort
// fallback to the SD source address), preserving prior behaviour for
// non-conformant peers instead of silently dropping the offer.
func TestHandleEntryOffer_FallsBackToSourceWhenNoOption(t *testing.T) {
	//fusa:test REQ-SDUDP-002
	d, err := NewDaemon(DaemonConfig{MulticastAddr: "239.192.255.251:0"})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	defer func() { _ = d.Close() }()

	entry := sd.Entry{
		Type:       sd.EntryTypeOffer,
		ServiceID:  0x5678,
		InstanceID: 0x0001,
		TTL:        60,
		// No option run: Index1/NumOpts1 are zero.
	}
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.5"), Port: 30490}

	d.handleEntry(entry, nil, from)

	records := d.Registry().Find(0x5678)
	if len(records) != 1 {
		t.Fatalf("Registry.Find: got %d records, want 1", len(records))
	}
	got := records[0].Endpoint
	if !got.IP.Equal(from.IP) || got.Port != uint16(from.Port) {
		t.Errorf("fallback endpoint = %+v, want IP=%v Port=%d (SD source)", got, from.IP, from.Port)
	}
}
