// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package sdudp implements SOME/IP Service Discovery (SOME/IP-SD) over real
// UDP sockets, including multicast announcements and unicast responses.
//
// A [Daemon] acts as a SOME/IP-SD agent on the local network. It can:
//
//   - Periodically broadcast OfferService messages on the AUTOSAR SD multicast
//     group (239.192.255.251:30490) for locally hosted services.
//   - Respond to FindService requests with a unicast OfferService reply.
//   - Update an in-process [sd.Registry] when remote offers are received.
//   - Send StopOffer (TTL=0) for all registered services on [Daemon.Close].
//
// # Eventgroup subscriptions
//
// A [Daemon] also handles SubscribeEventgroup / SubscribeEventgroupAck so
// that remote clients can subscribe to events from locally hosted services.
//
// # Integration tests
//
// Tests that exercise real sockets are gated behind the `integration` build tag:
//
//	go test -tags integration ./sd/udp/...
package sdudp

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
	"github.com/SoundMatt/go-SOMEIP/sd"
)

// AUTOSAR SOME/IP-SD multicast address and port.
const (
	DefaultMulticastAddr = "239.192.255.251:30490"
	DefaultTTL           = uint32(3)   // seconds
	DefaultOfferInterval = time.Second // re-offer period
)

// SD payload fixed-field sizes.
const (
	sdFlagsSize      = 4 // Flags (1) + Reserved (3)
	sdEntriesLenSize = 4
	sdOptionsLenSize = 4
)

// ── SD payload encode/decode ──────────────────────────────────────────────────

// sdMessage is a decoded SOME/IP-SD payload.
type sdMessage struct {
	Flags   uint8
	Entries []sd.Entry
	Options []sd.Option
}

// encodeSDFrame builds a complete SOME/IP-SD frame (header + SD payload) with
// a single entry and optional IPv4 endpoint option appended to dst.
//
//fusa:req REQ-SDUDP-001
func encodeSDFrame(dst []byte, flags uint8, entry sd.Entry, opt *sd.IPv4EndpointOption, optType uint8) []byte {
	// Build SD payload first so we know the length.
	var optsBuf []byte
	if opt != nil {
		optsBuf = sd.EncodeIPv4Option(optsBuf, optType, *opt)
		// Set option run indices on the entry.
		entry.Index1 = 0
		entry.NumOpts1 = 1
	}

	entriesBuf := sd.EncodeEntry(nil, entry)

	// SD payload layout:
	//   Flags(1) + Reserved(3) + EntriesLen(4) + Entries + OptionsLen(4) + Options
	payloadLen := sdFlagsSize + sdEntriesLenSize + len(entriesBuf) + sdOptionsLenSize + len(optsBuf)

	payload := make([]byte, 0, payloadLen)
	payload = append(payload, flags, 0x00, 0x00, 0x00) // Flags + Reserved
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(entriesBuf)))
	payload = append(payload, entriesBuf...)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(optsBuf)))
	payload = append(payload, optsBuf...)

	msg := someip.Message{
		ServiceID:       sd.SDServiceID,
		MethodID:        sd.SDMethodID,
		ProtocolVersion: 0x01,
		MessageType:     someip.MsgTypeNotification,
		ReturnCode:      someip.RetOK,
		Payload:         payload,
	}
	return codec.Encode(dst, msg)
}

// decodeSDPayload parses the SD payload section of a SOME/IP frame.
//
//fusa:req REQ-SDUDP-002
func decodeSDPayload(payload []byte) (sdMessage, error) {
	if len(payload) < sdFlagsSize+sdEntriesLenSize {
		return sdMessage{}, someip.ErrMalformedMessage
	}
	flags := payload[0]
	offset := sdFlagsSize

	entriesLen := int(binary.BigEndian.Uint32(payload[offset:]))
	offset += sdEntriesLenSize
	if offset+entriesLen > len(payload) {
		return sdMessage{}, someip.ErrMalformedMessage
	}

	var entries []sd.Entry
	entriesEnd := offset + entriesLen
	for offset < entriesEnd {
		if offset+sd.EntrySize > entriesEnd {
			break
		}
		e, err := sd.DecodeEntry(payload[offset:])
		if err != nil {
			break
		}
		entries = append(entries, e)
		offset += sd.EntrySize
	}

	// Options array follows the Entries array: a 4-byte length, then the
	// options themselves. Entries reference this array by index (Index1/
	// Index2, NumOpts1/NumOpts2), so it must be decoded for those indices to
	// resolve to anything meaningful.
	offset = entriesEnd
	if offset+sdOptionsLenSize > len(payload) {
		return sdMessage{}, someip.ErrMalformedMessage
	}
	optionsLen := int(binary.BigEndian.Uint32(payload[offset:]))
	offset += sdOptionsLenSize
	if optionsLen < 0 || offset+optionsLen > len(payload) {
		return sdMessage{}, someip.ErrMalformedMessage
	}
	options, err := sd.DecodeOptions(payload[offset : offset+optionsLen])
	if err != nil {
		return sdMessage{}, err
	}

	return sdMessage{Flags: flags, Entries: entries, Options: options}, nil
}

// ── Daemon ────────────────────────────────────────────────────────────────────

// OfferConfig describes a service to be periodically announced.
type OfferConfig struct {
	// Entry is the SD entry describing the service (type, IDs, TTL, version).
	Entry sd.Entry
	// Endpoint is the IPv4 endpoint where the service is reachable.
	Endpoint sd.IPv4EndpointOption
	// EndpointProtocol is OptionTypeIPv4Endpoint (0x04) for UDP or TCP.
	// Defaults to sd.OptionTypeIPv4Endpoint (0x04).
	EndpointProtocol uint8
	// OfferInterval overrides the per-service re-offer period.
	// Zero means use DaemonConfig.OfferInterval.
	OfferInterval time.Duration
}

// SubscribeHandler is called when a remote SubscribeEventgroup is received.
// The implementation should record the subscriber address and begin sending
// event notifications to it.
type SubscribeHandler func(entry sd.Entry, from net.Addr)

// DaemonConfig configures a [Daemon].
type DaemonConfig struct {
	// MulticastAddr is the SD multicast address (default 239.192.255.251:30490).
	MulticastAddr string
	// UnicastAddr is the local unicast address for receiving unicast SD messages.
	// Empty binds an ephemeral port on all interfaces.
	UnicastAddr string
	// OfferInterval is the default period between periodic OfferService messages.
	// Zero uses [DefaultOfferInterval].
	OfferInterval time.Duration
	// OnSubscribe is called when a SubscribeEventgroup entry is received.
	// May be nil.
	OnSubscribe SubscribeHandler
}

type offerKey struct {
	serviceID  someip.ServiceID
	instanceID someip.InstanceID
}

type offerRecord struct {
	cfg    OfferConfig
	ticker *time.Ticker
	done   chan struct{}
}

// Daemon is a SOME/IP-SD agent that uses real UDP sockets.
// A Daemon is safe for concurrent use from multiple goroutines.
//
//fusa:req REQ-SDUDP-003
type Daemon struct {
	cfg      DaemonConfig
	mcAddr   *net.UDPAddr // multicast group address
	conn     *net.UDPConn // shared socket (bound to unicast, joined multicast group)
	registry *sd.Registry

	mu     sync.RWMutex
	offers map[offerKey]*offerRecord

	closed atomic.Bool
	wg     sync.WaitGroup
}

// NewDaemon creates and starts a SOME/IP-SD daemon.
//
//fusa:req REQ-SDUDP-004
func NewDaemon(cfg DaemonConfig) (*Daemon, error) {
	if cfg.MulticastAddr == "" {
		cfg.MulticastAddr = DefaultMulticastAddr
	}
	if cfg.OfferInterval == 0 {
		cfg.OfferInterval = DefaultOfferInterval
	}

	mcAddr, err := net.ResolveUDPAddr("udp4", cfg.MulticastAddr)
	if err != nil {
		return nil, err
	}

	// Join the multicast group so we receive SD announcements from peers.
	conn, err := net.ListenMulticastUDP("udp4", nil, mcAddr) //nolint:noctx
	if err != nil {
		return nil, err
	}

	d := &Daemon{
		cfg:      cfg,
		mcAddr:   mcAddr,
		conn:     conn,
		registry: sd.NewRegistry(),
		offers:   make(map[offerKey]*offerRecord),
	}
	d.wg.Add(1)
	go d.readLoop()
	return d, nil
}

// Registry returns the live in-process service registry, which is updated
// whenever a remote OfferService or StopOfferService message is received.
//
//fusa:req REQ-SDUDP-005
func (d *Daemon) Registry() *sd.Registry { return d.registry }

// Offer registers a service offer and begins sending periodic OfferService
// announcements on the SD multicast group. A stop-offer (TTL=0) is sent
// automatically when [Daemon.Close] is called.
//
//fusa:req REQ-SDUDP-006
func (d *Daemon) Offer(cfg OfferConfig) {
	if d.closed.Load() {
		return
	}
	cfg.Entry.Type = sd.EntryTypeOffer
	if cfg.Entry.TTL == 0 {
		cfg.Entry.TTL = DefaultTTL
	}
	if cfg.EndpointProtocol == 0 {
		cfg.EndpointProtocol = sd.OptionTypeIPv4Endpoint
	}
	interval := cfg.OfferInterval
	if interval == 0 {
		interval = d.cfg.OfferInterval
	}

	key := offerKey{cfg.Entry.ServiceID, cfg.Entry.InstanceID}

	// Send initial offer immediately.
	d.sendOffer(cfg, false)

	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	rec := &offerRecord{cfg: cfg, ticker: ticker, done: done}

	d.mu.Lock()
	if old, ok := d.offers[key]; ok {
		old.ticker.Stop()
		close(old.done)
	}
	d.offers[key] = rec
	d.mu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			select {
			case <-ticker.C:
				if d.closed.Load() {
					return
				}
				d.sendOffer(cfg, false)
			case <-done:
				return
			}
		}
	}()
}

// Find sends a FindService message on the multicast group for serviceID.
// Responses arrive asynchronously and are applied to the [Daemon.Registry].
//
//fusa:req REQ-SDUDP-007
func (d *Daemon) Find(serviceID someip.ServiceID) {
	if d.closed.Load() {
		return
	}
	entry := sd.Entry{
		Type:         sd.EntryTypeFind,
		ServiceID:    serviceID,
		InstanceID:   0xFFFF,
		MajorVersion: 0xFF,
		TTL:          DefaultTTL,
		MinorVersion: 0xFFFFFFFF,
	}
	frame := encodeSDFrame(nil, sd.FlagUnicast, entry, nil, 0)
	_, _ = d.conn.WriteToUDP(frame, d.mcAddr)
}

// Close sends StopOffer for all registered services and shuts down the daemon.
//
//fusa:req REQ-SDUDP-008
func (d *Daemon) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Stop all offer tickers, wake their goroutines, and send StopOffer (TTL=0).
	d.mu.Lock()
	for _, rec := range d.offers {
		rec.ticker.Stop()
		close(rec.done)
		stop := rec.cfg
		stop.Entry.TTL = 0
		d.sendOffer(stop, true)
	}
	d.mu.Unlock()

	err := d.conn.Close()
	d.wg.Wait()
	return err
}

func (d *Daemon) sendOffer(cfg OfferConfig, stopOffer bool) {
	entry := cfg.Entry
	if stopOffer {
		entry.TTL = 0
	}
	var opt *sd.IPv4EndpointOption
	optType := cfg.EndpointProtocol
	if !stopOffer && cfg.Endpoint.IP != nil {
		ep := cfg.Endpoint
		opt = &ep
	}
	frame := encodeSDFrame(nil, sd.FlagReboot, entry, opt, optType)
	_, _ = d.conn.WriteToUDP(frame, d.mcAddr)
}

func (d *Daemon) readLoop() {
	defer d.wg.Done()
	buf := make([]byte, 65507)
	for {
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if d.closed.Load() {
				return
			}
			continue
		}
		frame := make([]byte, n)
		copy(frame, buf[:n])
		go d.handleFrame(frame, addr)
	}
}

func (d *Daemon) handleFrame(frame []byte, from *net.UDPAddr) {
	msg, err := codec.Decode(frame)
	if err != nil {
		return
	}
	if msg.ServiceID != sd.SDServiceID || msg.MethodID != sd.SDMethodID {
		return
	}

	sdMsg, err := decodeSDPayload(msg.Payload)
	if err != nil {
		return
	}

	for _, entry := range sdMsg.Entries {
		d.handleEntry(entry, sdMsg.Options, from)
	}
}

func (d *Daemon) handleEntry(entry sd.Entry, opts []sd.Option, from *net.UDPAddr) {
	switch entry.Type {
	case sd.EntryTypeOffer:
		// The offer's actual endpoint (IP/port/L4-protocol) is carried by the
		// IPv4 endpoint option its entry references via Index1/NumOpts1 (or
		// Index2/NumOpts2), not the SD source socket the offer happened to
		// arrive on — those are typically the same host but the SD multicast
		// source port (30490) is not generally the offered service's port,
		// and a TCP-offered service must not be registered as UDP.
		endpoint, ok := sd.ResolveIPv4Endpoint(entry, opts)
		if !ok {
			// No decodable endpoint option referenced: fall back to the SD
			// source address/port (best-effort for a non-conformant offer)
			// rather than dropping the offer entirely.
			endpoint = sd.IPv4EndpointOption{
				IP:       from.IP,
				Protocol: 0x11, // UDP
				Port:     uint16(from.Port),
			}
		}
		d.registry.Offer(entry, endpoint)

	case sd.EntryTypeFind:
		// Respond with our offers matching the requested serviceID.
		d.mu.RLock()
		var matches []*offerRecord
		for key, rec := range d.offers {
			if entry.ServiceID == 0xFFFF || key.serviceID == entry.ServiceID {
				matches = append(matches, rec)
			}
		}
		d.mu.RUnlock()
		for _, rec := range matches {
			frame := encodeSDFrame(nil, sd.FlagReboot, rec.cfg.Entry, &rec.cfg.Endpoint, rec.cfg.EndpointProtocol)
			_, _ = d.conn.WriteToUDP(frame, from)
		}

	case sd.EntryTypeSubscribe:
		if d.cfg.OnSubscribe != nil {
			d.cfg.OnSubscribe(entry, from)
		}
		// Send SubscribeAck.
		ack := sd.Entry{
			Type:         sd.EntryTypeSubscribeAck,
			ServiceID:    entry.ServiceID,
			InstanceID:   entry.InstanceID,
			MajorVersion: entry.MajorVersion,
			TTL:          entry.TTL,
			EventgroupID: entry.EventgroupID,
		}
		frame := encodeSDFrame(nil, 0x00, ack, nil, 0)
		_, _ = d.conn.WriteToUDP(frame, from)
	}
}

// SubscribeEventgroup sends a SubscribeEventgroup entry to the given server address.
// The server is expected to respond with a SubscribeEventgroupAck.
//
//fusa:req REQ-SDUDP-009
func (d *Daemon) SubscribeEventgroup(ctx context.Context, serverAddr string, entry sd.Entry) error {
	if d.closed.Load() {
		return someip.ErrClosed
	}
	addr, err := net.ResolveUDPAddr("udp4", serverAddr)
	if err != nil {
		return err
	}
	sub := entry
	sub.Type = sd.EntryTypeSubscribe
	if sub.TTL == 0 {
		sub.TTL = DefaultTTL
	}
	frame := encodeSDFrame(nil, sd.FlagUnicast, sub, nil, 0)
	_, err = d.conn.WriteToUDP(frame, addr)
	_ = ctx
	return err
}
