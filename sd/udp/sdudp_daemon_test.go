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
