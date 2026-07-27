// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package udp provides a pure-Go SOME/IP transport over UDP.
//
// SOME/IP over UDP is used for unreliable delivery: fire-and-forget
// notifications, low-latency method calls, and multicast event distribution.
// For reliable delivery use the tcp package instead.
//
// # Server
//
// A [Server] listens on a UDP port, dispatches incoming requests to registered
// handlers, and sends responses back to the originating address.
//
// # Service
//
// A [Service] sends SOME/IP requests to a remote server address and
// correlates responses by SessionID. Concurrent calls to [Service.Call]
// are safe; each gets a unique SessionID.
//
// # Integration tests
//
// Tests that exercise real sockets are gated behind the `integration` build tag:
//
//	go test -tags integration ./udp/...
package udp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
	"github.com/SoundMatt/go-SOMEIP/sd"
)

const (
	defaultTimeout  = 5 * time.Second
	maxUDPFrameSize = 65507 // IPv4 UDP payload limit
)

// ServerConfig configures a UDP [Server].
type ServerConfig struct {
	// Addr is the UDP address to listen on (e.g. "0.0.0.0:30509").
	Addr string
	// ServiceID identifies the service hosted by this server.
	ServiceID someip.ServiceID
	// InstanceID identifies the service instance.
	InstanceID someip.InstanceID
	// InterfaceVersion is the major version of the hosted interface.
	InterfaceVersion uint8
}

// Server is a SOME/IP server that listens on a UDP socket.
// A Server is safe for concurrent use from multiple goroutines.
type Server struct {
	cfg      ServerConfig
	conn     *net.UDPConn
	mu       sync.RWMutex
	handlers map[someip.MethodID]someip.MethodHandler
	subs     map[someip.EventID][]net.Addr
	closed   atomic.Bool
	wg       sync.WaitGroup
}

//fusa:req REQ-UDP-001

// NewServer creates and starts a SOME/IP UDP server.
// The server begins listening immediately; call [Server.Close] to stop it.
func NewServer(cfg ServerConfig) (*Server, error) {
	addr, err := net.ResolveUDPAddr("udp4", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("udp: resolve %q: %w", cfg.Addr, err)
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("udp: listen %q: %w", cfg.Addr, err)
	}

	s := &Server{
		cfg:      cfg,
		conn:     conn,
		handlers: make(map[someip.MethodID]someip.MethodHandler),
		subs:     make(map[someip.EventID][]net.Addr),
	}
	s.wg.Add(1)
	go s.readLoop()
	return s, nil
}

//fusa:req REQ-UDP-002

// RegisterMethod registers handler for methodID.
func (s *Server) RegisterMethod(methodID someip.MethodID, handler someip.MethodHandler) error {
	if s.closed.Load() {
		return someip.ErrClosed
	}
	s.mu.Lock()
	s.handlers[methodID] = handler
	s.mu.Unlock()
	return nil
}

//fusa:req REQ-UDP-003

// Emit sends a SOME/IP notification for eventID to all registered subscriber addresses.
func (s *Server) Emit(eventID someip.EventID, payload []byte) error {
	if s.closed.Load() {
		return someip.ErrClosed
	}

	msg := someip.Message{
		ServiceID:        s.cfg.ServiceID,
		MethodID:         someip.MethodID(eventID),
		ProtocolVersion:  0x01,
		InterfaceVersion: s.cfg.InterfaceVersion,
		MessageType:      someip.MsgTypeNotification,
		ReturnCode:       someip.RetOK,
		Payload:          payload,
	}
	frame := codec.Encode(nil, msg)

	s.mu.RLock()
	addrs := append([]net.Addr(nil), s.subs[eventID]...)
	s.mu.RUnlock()

	for _, addr := range addrs {
		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}
		_, _ = s.conn.WriteToUDP(frame, udpAddr)
	}
	return nil
}

//fusa:req REQ-UDP-013

// RegisterSubscriber records addr as a subscriber for eventID so that future
// [Server.Emit] calls deliver to it. This is the wiring point for an external
// SOME/IP-SD daemon (see the sd/udp package): pass its DaemonConfig.OnSubscribe
// handler a closure that calls RegisterSubscriber with the entry's EventgroupID
// and the sender's address. It is also called automatically when a Subscribe
// entry arrives directly on the data socket (see [Service.Subscribe]).
// Duplicate registrations for the same eventID/addr pair are no-ops.
func (s *Server) RegisterSubscriber(eventID someip.EventID, addr net.Addr) error {
	if s.closed.Load() {
		return someip.ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.subs[eventID] {
		if existing.String() == addr.String() {
			return nil
		}
	}
	s.subs[eventID] = append(s.subs[eventID], addr)
	return nil
}

//fusa:req REQ-UDP-014

// UnregisterSubscriber removes addr from eventID's subscriber list. It is a
// no-op if addr was not registered.
func (s *Server) UnregisterSubscriber(eventID someip.EventID, addr net.Addr) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.subs[eventID]
	filtered := make([]net.Addr, 0, len(existing))
	for _, a := range existing {
		if a.String() != addr.String() {
			filtered = append(filtered, a)
		}
	}
	s.subs[eventID] = filtered
	return nil
}

//fusa:req REQ-UDP-004

// Close stops the server and releases the UDP socket.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := s.conn.Close()
	s.wg.Wait()
	return err
}

// LocalAddr returns the local UDP address the server is bound to.
func (s *Server) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *Server) readLoop() {
	defer s.wg.Done()
	buf := make([]byte, maxUDPFrameSize)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		frame := make([]byte, n)
		copy(frame, buf[:n])
		go s.handleFrame(frame, addr)
	}
}

func (s *Server) handleFrame(frame []byte, addr *net.UDPAddr) {
	msg, err := codec.Decode(frame)
	if err != nil {
		return
	}

	if msg.ServiceID == sd.SDServiceID && msg.MethodID == sd.SDMethodID {
		s.handleSubscribeFrame(msg, addr)
		return
	}

	s.mu.RLock()
	handler, ok := s.handlers[msg.MethodID]
	s.mu.RUnlock()

	if msg.MessageType == someip.MsgTypeRequestNoReturn {
		if ok {
			go func() { _, _ = handler(context.Background(), msg) }()
		}
		return
	}

	if !ok {
		resp := someip.Message{
			ServiceID:        s.cfg.ServiceID,
			MethodID:         msg.MethodID,
			ClientID:         msg.ClientID,
			SessionID:        msg.SessionID,
			ProtocolVersion:  0x01,
			InterfaceVersion: s.cfg.InterfaceVersion,
			MessageType:      someip.MsgTypeError,
			ReturnCode:       someip.RetUnknownMethod,
		}
		frame := codec.Encode(nil, resp)
		_, _ = s.conn.WriteToUDP(frame, addr)
		return
	}

	respPayload, handlerErr := handler(context.Background(), msg)
	resp := someip.Message{
		ServiceID:        s.cfg.ServiceID,
		MethodID:         msg.MethodID,
		ClientID:         msg.ClientID,
		SessionID:        msg.SessionID,
		ProtocolVersion:  0x01,
		InterfaceVersion: s.cfg.InterfaceVersion,
	}
	if handlerErr != nil {
		resp.MessageType = someip.MsgTypeError
		resp.ReturnCode = someip.RetNotOK
	} else {
		resp.MessageType = someip.MsgTypeResponse
		resp.ReturnCode = someip.RetOK
		resp.Payload = respPayload
	}
	frame2 := codec.Encode(nil, resp)
	_, _ = s.conn.WriteToUDP(frame2, addr)
}

// handleSubscribeFrame processes a SOME/IP-SD Subscribe/Unsubscribe entry
// received directly on the data socket (see encodeSDEntryFrame), registers or
// unregisters addr as a subscriber, and replies with a SubscribeAck.
func (s *Server) handleSubscribeFrame(msg someip.Message, addr *net.UDPAddr) {
	entry, ok := decodeSubscribeEntry(msg.Payload)
	if !ok {
		return
	}
	eventID := someip.EventID(entry.EventgroupID)
	if entry.TTL == 0 {
		_ = s.UnregisterSubscriber(eventID, addr)
	} else {
		_ = s.RegisterSubscriber(eventID, addr)
	}
	ack := encodeSDEntryFrame(sd.EntryTypeSubscribeAck, s.cfg.ServiceID, s.cfg.InstanceID, eventID, entry.TTL)
	_, _ = s.conn.WriteToUDP(ack, addr)
}

// ── SOME/IP-SD subscribe wiring ─────────────────────────────────────────────
//
// Server and Service exchange a minimal point-to-point SOME/IP-SD
// Subscribe/SubscribeAck handshake directly over the data socket, so that
// [Service.Subscribe] actually registers the caller's address with the
// [Server] before returning and [Server.Emit] has subscribers to deliver to.
// Applications that also run a full sd/udp.Daemon (multicast offers/finds)
// can additionally wire its OnSubscribe handler to [Server.RegisterSubscriber]
// for eventgroup subscriptions announced there instead of directly here.
//
// This Server does not expire subscriptions by TTL; TTL is carried on the
// wire for protocol compatibility only. A TTL of zero unsubscribes.

// sdPayloadHeaderSize is the Flags(1)+Reserved(3)+EntriesLength(4) prefix of
// a SOME/IP-SD payload, before the entries themselves.
const sdPayloadHeaderSize = 4 + 4

// encodeSDEntryFrame builds a complete SOME/IP-SD wire frame (SOME/IP header
// + SD payload) carrying a single entry of entryType for eventID, with no
// options. See sd.Entry / codec.Encode for the wire layouts.
func encodeSDEntryFrame(entryType uint8, serviceID someip.ServiceID, instanceID someip.InstanceID, eventID someip.EventID, ttl uint32) []byte {
	entry := sd.Entry{
		Type:         entryType,
		ServiceID:    serviceID,
		InstanceID:   instanceID,
		MajorVersion: 0xFF,
		TTL:          ttl,
		EventgroupID: uint16(eventID),
	}
	entryBuf := sd.EncodeEntry(nil, entry)

	payload := make([]byte, 0, sdPayloadHeaderSize+len(entryBuf)+4)
	payload = append(payload, 0x00, 0x00, 0x00, 0x00) // Flags + Reserved
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(entryBuf)))
	payload = append(payload, entryBuf...)
	payload = binary.BigEndian.AppendUint32(payload, 0) // no options

	msg := someip.Message{
		ServiceID:       sd.SDServiceID,
		MethodID:        sd.SDMethodID,
		ProtocolVersion: someip.SOMEIPProtocolVersion,
		MessageType:     someip.MsgTypeNotification,
		ReturnCode:      someip.RetOK,
		Payload:         payload,
	}
	return codec.Encode(nil, msg)
}

// decodeSubscribeEntry extracts a Subscribe/SubscribeAck entry from a SD
// payload built by encodeSDEntryFrame. ok is false if the payload is too
// short or malformed.
func decodeSubscribeEntry(payload []byte) (entry sd.Entry, ok bool) {
	if len(payload) < sdPayloadHeaderSize {
		return sd.Entry{}, false
	}
	entriesLen := int(binary.BigEndian.Uint32(payload[4:8]))
	if entriesLen < sd.EntrySize || sdPayloadHeaderSize+sd.EntrySize > len(payload) {
		return sd.Entry{}, false
	}
	e, err := sd.DecodeEntry(payload[sdPayloadHeaderSize:])
	if err != nil {
		return sd.Entry{}, false
	}
	if e.Type != sd.EntryTypeSubscribe && e.Type != sd.EntryTypeSubscribeAck {
		return sd.Entry{}, false
	}
	return e, true
}

// ── Service ───────────────────────────────────────────────────────────────────

// ServiceConfig configures a UDP [Service].
type ServiceConfig struct {
	// ServerAddr is the UDP address of the remote server (e.g. "10.0.0.1:30509").
	ServerAddr string
	// ServiceID identifies the target service.
	ServiceID someip.ServiceID
	// InstanceID identifies the target instance.
	InstanceID someip.InstanceID
	// Timeout is the per-call deadline. Zero uses the default (5 s).
	Timeout time.Duration
}

// Service is a SOME/IP UDP client.
// A Service is safe for concurrent use from multiple goroutines.
type Service struct {
	cfg        ServiceConfig
	conn       *net.UDPConn
	serverAddr *net.UDPAddr
	timeout    time.Duration

	mu        sync.Mutex
	sessionID uint16
	pending   map[someip.SessionID]chan someip.Message

	// subs is guarded by mu (not a sync.Map): sync.Map.CompareAndSwap
	// requires a comparable value type, and []chan someip.Message is not
	// comparable — a slice-valued sync.Map panics under concurrent
	// Subscribe calls for the same eventID. Every mutation below replaces
	// the slice with a fresh backing array (copy-on-write) so dispatchFrame
	// can safely range over a snapshot taken under mu without it being
	// mutated in place by a concurrent Subscribe/Unsubscribe.
	subs   map[someip.EventID][]chan someip.Message
	closed atomic.Bool
	wg     sync.WaitGroup
}

//fusa:req REQ-UDP-005

// NewService creates a SOME/IP UDP client connected to a remote server.
func NewService(cfg ServiceConfig) (*Service, error) {
	serverAddr, err := net.ResolveUDPAddr("udp4", cfg.ServerAddr)
	if err != nil {
		return nil, fmt.Errorf("udp: resolve server %q: %w", cfg.ServerAddr, err)
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("udp: dial: %w", err)
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	svc := &Service{
		cfg:        cfg,
		conn:       conn,
		serverAddr: serverAddr,
		timeout:    timeout,
		pending:    make(map[someip.SessionID]chan someip.Message),
		subs:       make(map[someip.EventID][]chan someip.Message),
	}
	svc.wg.Add(1)
	go svc.readLoop()
	return svc, nil
}

func (svc *Service) nextSession() someip.SessionID {
	svc.mu.Lock()
	svc.sessionID++
	id := svc.sessionID
	svc.mu.Unlock()
	return someip.SessionID(id)
}

//fusa:req REQ-UDP-006

// Call sends a SOME/IP request and waits for the response.
func (svc *Service) Call(ctx context.Context, methodID someip.MethodID, payload []byte) (someip.Message, error) {
	if svc.closed.Load() {
		return someip.Message{}, someip.ErrClosed
	}

	sessionID := svc.nextSession()
	ch := make(chan someip.Message, 1)

	svc.mu.Lock()
	svc.pending[sessionID] = ch
	svc.mu.Unlock()

	defer func() {
		svc.mu.Lock()
		delete(svc.pending, sessionID)
		svc.mu.Unlock()
	}()

	req := someip.Message{
		ServiceID:   svc.cfg.ServiceID,
		MethodID:    methodID,
		SessionID:   sessionID,
		ClientID:    0x0001,
		MessageType: someip.MsgTypeRequest,
		ReturnCode:  someip.RetOK,
		Payload:     payload,
	}
	frame := codec.Encode(nil, req)
	if _, err := svc.conn.WriteToUDP(frame, svc.serverAddr); err != nil {
		return someip.Message{}, fmt.Errorf("udp: send: %w", err)
	}

	deadline := time.NewTimer(svc.timeout)
	defer deadline.Stop()

	select {
	case resp := <-ch:
		return resp, nil
	case <-deadline.C:
		return someip.Message{}, someip.ErrTimeout
	case <-ctx.Done():
		return someip.Message{}, ctx.Err()
	}
}

//fusa:req REQ-UDP-007

// CallNoReturn sends a fire-and-forget SOME/IP request.
func (svc *Service) CallNoReturn(ctx context.Context, methodID someip.MethodID, payload []byte) error {
	if svc.closed.Load() {
		return someip.ErrClosed
	}
	req := someip.Message{
		ServiceID:   svc.cfg.ServiceID,
		MethodID:    methodID,
		SessionID:   svc.nextSession(),
		ClientID:    0x0001,
		MessageType: someip.MsgTypeRequestNoReturn,
		ReturnCode:  someip.RetOK,
		Payload:     payload,
	}
	frame := codec.Encode(nil, req)
	_, err := svc.conn.WriteToUDP(frame, svc.serverAddr)
	return err
}

// subscribeTTL is the TTL (seconds) carried on outbound Subscribe entries.
// This Service/Server pair does not expire subscriptions by TTL — call
// [Subscription.Unsubscribe] to remove one — so the value is informational,
// carried for wire compatibility with real SOME/IP-SD peers only.
const subscribeTTL = 3600

//fusa:req REQ-UDP-008

// Subscribe creates a subscription for event notifications and registers
// this service's local address with the server so that [Server.Emit] will
// deliver to it (RELAY spec §8.6). It returns an error, without creating the
// subscription, if the registration request cannot be sent.
//
// UDP subscriptions receive notifications emitted by the server to this
// service's local UDP address.
func (svc *Service) Subscribe(eventID someip.EventID, opts ...someip.SubscriberOption) (someip.Subscription, error) {
	if svc.closed.Load() {
		return nil, someip.ErrClosed
	}
	cfg := someip.ApplySubscriberOpts(opts)
	ch := make(chan someip.Message, cfg.ChanDepth(64))

	svc.mu.Lock()
	old := svc.subs[eventID]
	grown := make([]chan someip.Message, len(old)+1)
	copy(grown, old)
	grown[len(old)] = ch
	svc.subs[eventID] = grown
	svc.mu.Unlock()

	frame := encodeSDEntryFrame(sd.EntryTypeSubscribe, svc.cfg.ServiceID, svc.cfg.InstanceID, eventID, subscribeTTL)
	if _, err := svc.conn.WriteToUDP(frame, svc.serverAddr); err != nil {
		sub := &udpSubscription{svc: svc, eventID: eventID, ch: ch}
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("udp: send subscribe: %w", err)
	}

	sub := &udpSubscription{svc: svc, eventID: eventID, ch: ch}
	return sub, nil
}

//fusa:req REQ-UDP-009

// Close stops the service.
func (svc *Service) Close() error {
	if !svc.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := svc.conn.Close()
	svc.wg.Wait()
	return err
}

func (svc *Service) readLoop() {
	defer svc.wg.Done()
	buf := make([]byte, maxUDPFrameSize)
	for {
		n, _, err := svc.conn.ReadFromUDP(buf)
		if err != nil {
			if svc.closed.Load() {
				return
			}
			continue
		}
		frame := make([]byte, n)
		copy(frame, buf[:n])
		svc.dispatchFrame(frame)
	}
}

func (svc *Service) dispatchFrame(frame []byte) {
	msg, err := codec.Decode(frame)
	if err != nil {
		return
	}

	if msg.ServiceID == sd.SDServiceID && msg.MethodID == sd.SDMethodID {
		// SubscribeAck / Subscribe control traffic; no client-side action needed.
		return
	}

	switch msg.MessageType {
	case someip.MsgTypeNotification:
		// Hold mu for the snapshot AND the sends (not just the snapshot):
		// Unsubscribe removes a channel from subs and then Close closes it,
		// both under mu. If we released mu before sending, a channel could
		// be removed-and-closed between our snapshot and our send, causing
		// a send on a closed channel. Serializing against Unsubscribe like
		// this guarantees any channel in our snapshot is not closed until
		// after we're done with it (sends are non-blocking, so the lock is
		// held only briefly).
		svc.mu.Lock()
		for _, ch := range svc.subs[someip.EventID(msg.MethodID)] {
			select {
			case ch <- msg:
			default:
			}
		}
		svc.mu.Unlock()
	case someip.MsgTypeResponse, someip.MsgTypeError:
		svc.mu.Lock()
		ch, ok := svc.pending[msg.SessionID]
		svc.mu.Unlock()
		if ok {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

// ── subscription ──────────────────────────────────────────────────────────────

type udpSubscription struct {
	svc     *Service
	eventID someip.EventID
	ch      chan someip.Message
	once    sync.Once
	closed  atomic.Bool
}

//fusa:req REQ-UDP-010
func (s *udpSubscription) C() <-chan someip.Message { return s.ch }

//fusa:req REQ-UDP-011
func (s *udpSubscription) Unsubscribe() error {
	s.svc.mu.Lock()
	chans := s.svc.subs[s.eventID]
	// Build filtered into a fresh backing array rather than mutating chans
	// in place: dispatchFrame may concurrently hold (and range over) a
	// slice snapshot taken under this same mutex, so writing into that
	// array's memory here — even under the lock, since dispatchFrame's
	// range happens after it has released the lock — would be an
	// unsynchronized concurrent read/write of shared memory.
	filtered := make([]chan someip.Message, 0, len(chans))
	for _, c := range chans {
		if c != s.ch {
			filtered = append(filtered, c)
		}
	}
	s.svc.subs[s.eventID] = filtered
	s.svc.mu.Unlock()

	frame := encodeSDEntryFrame(sd.EntryTypeSubscribe, s.svc.cfg.ServiceID, s.svc.cfg.InstanceID, s.eventID, 0)
	_, _ = s.svc.conn.WriteToUDP(frame, s.svc.serverAddr)
	return nil
}

//fusa:req REQ-UDP-012
func (s *udpSubscription) Close() error {
	_ = s.Unsubscribe()
	s.once.Do(func() {
		s.closed.Store(true)
		close(s.ch)
	})
	return nil
}
