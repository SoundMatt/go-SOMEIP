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
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
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
		MessageType:      someip.Notification,
		ReturnCode:       someip.OK,
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

	s.mu.RLock()
	handler, ok := s.handlers[msg.MethodID]
	s.mu.RUnlock()

	if msg.MessageType == someip.RequestNoReturn {
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
			MessageType:      someip.Error,
			ReturnCode:       someip.UnknownMethod,
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
		resp.MessageType = someip.Error
		resp.ReturnCode = someip.NotOK
	} else {
		resp.MessageType = someip.Response
		resp.ReturnCode = someip.OK
		resp.Payload = respPayload
	}
	frame2 := codec.Encode(nil, resp)
	_, _ = s.conn.WriteToUDP(frame2, addr)
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
	cfg       ServiceConfig
	conn      *net.UDPConn
	serverAddr *net.UDPAddr
	timeout   time.Duration

	mu        sync.Mutex
	sessionID uint16
	pending   map[someip.SessionID]chan someip.Message

	subs      sync.Map // EventID → []chan someip.Message
	closed    atomic.Bool
	wg        sync.WaitGroup
}

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
		MessageType: someip.Request,
		ReturnCode:  someip.OK,
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
		MessageType: someip.RequestNoReturn,
		ReturnCode:  someip.OK,
		Payload:     payload,
	}
	frame := codec.Encode(nil, req)
	_, err := svc.conn.WriteToUDP(frame, svc.serverAddr)
	return err
}

// Subscribe creates a subscription for event notifications.
// UDP subscriptions receive notifications emitted by the server to this
// service's local UDP address.
func (svc *Service) Subscribe(eventID someip.EventID, opts ...someip.SubscribeOption) (someip.Subscription, error) {
	if svc.closed.Load() {
		return nil, someip.ErrClosed
	}
	cfg := someip.ApplySubscribeOpts(opts)
	ch := make(chan someip.Message, cfg.ChanDepth(64))

	for {
		actual, loaded := svc.subs.LoadOrStore(eventID, []chan someip.Message{ch})
		if !loaded {
			break
		}
		old, ok := actual.([]chan someip.Message)
		if !ok {
			break
		}
		if svc.subs.CompareAndSwap(eventID, actual, append(old, ch)) {
			break
		}
	}

	sub := &udpSubscription{svc: svc, eventID: eventID, ch: ch}
	return sub, nil
}

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

	switch msg.MessageType {
	case someip.Notification:
		if val, ok := svc.subs.Load(msg.MethodID); ok {
			if chans, ok := val.([]chan someip.Message); ok {
				for _, ch := range chans {
					select {
					case ch <- msg:
					default:
					}
				}
			}
		}
	case someip.Response, someip.Error:
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

func (s *udpSubscription) C() <-chan someip.Message { return s.ch }

func (s *udpSubscription) Unsubscribe() error {
	if val, ok := s.svc.subs.Load(s.eventID); ok {
		if chans, ok := val.([]chan someip.Message); ok {
			filtered := chans[:0]
			for _, c := range chans {
				if c != s.ch {
					filtered = append(filtered, c)
				}
			}
			s.svc.subs.Store(s.eventID, filtered)
		}
	}
	return nil
}

func (s *udpSubscription) Close() error {
	_ = s.Unsubscribe()
	s.once.Do(func() {
		s.closed.Store(true)
		close(s.ch)
	})
	return nil
}
