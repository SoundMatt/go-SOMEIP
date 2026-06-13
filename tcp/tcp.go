// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package tcp provides a pure-Go SOME/IP transport over TCP.
//
// SOME/IP over TCP is used for reliable delivery: method calls that require
// guaranteed receipt, large payloads (combined with SOME/IP-TP), and
// persistent service connections.
//
// # Server
//
// A [Server] listens on a TCP address and accepts multiple concurrent client
// connections. Each accepted connection gets its own read loop; incoming
// requests are dispatched to registered handlers and responses are written
// back on the same connection.
//
// # Service
//
// A [Service] maintains a single persistent TCP connection to a remote server.
// Concurrent calls to [Service.Call] are multiplexed over that connection by
// SessionID. If the connection drops, calls return [someip.ErrNotReady].
//
// # Integration tests
//
// Tests that exercise real sockets are gated behind the `integration` build tag:
//
//	go test -tags integration ./tcp/...
package tcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
)

const (
	defaultTimeout     = 5 * time.Second
	defaultDialTimeout = 10 * time.Second
)

// ServerConfig configures a TCP [Server].
type ServerConfig struct {
	// Addr is the TCP address to listen on (e.g. "0.0.0.0:30509").
	Addr string
	// ServiceID identifies the service hosted by this server.
	ServiceID someip.ServiceID
	// InstanceID identifies the service instance.
	InstanceID someip.InstanceID
	// InterfaceVersion is the major version of the hosted interface.
	InterfaceVersion uint8
}

// Server is a SOME/IP server that listens on a TCP socket.
// A Server is safe for concurrent use from multiple goroutines.
//
//fusa:req REQ-TCP-001
type Server struct {
	cfg      ServerConfig
	ln       net.Listener
	mu       sync.RWMutex
	handlers map[someip.MethodID]someip.MethodHandler
	closed   atomic.Bool
	wg       sync.WaitGroup
}

// NewServer creates and starts a SOME/IP TCP server.
// The server begins accepting connections immediately; call [Server.Close] to stop it.
func NewServer(cfg ServerConfig) (*Server, error) {
	ln, err := net.Listen("tcp4", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("tcp: listen %q: %w", cfg.Addr, err)
	}
	s := &Server{
		cfg:      cfg,
		ln:       ln,
		handlers: make(map[someip.MethodID]someip.MethodHandler),
	}
	s.wg.Add(1)
	go s.acceptLoop()
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

// Emit is not supported over TCP (TCP SOME/IP uses subscription-based SD).
// Returns nil without sending anything.
func (s *Server) Emit(_ someip.EventID, _ []byte) error {
	if s.closed.Load() {
		return someip.ErrClosed
	}
	return nil
}

// Close stops the server and closes the listener.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := s.ln.Close()
	s.wg.Wait()
	return err
}

// LocalAddr returns the local TCP address the server is listening on.
func (s *Server) LocalAddr() net.Addr {
	return s.ln.Addr()
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		s.wg.Add(1)
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close() //nolint:errcheck
	for {
		msg, err := readFrame(conn)
		if err != nil {
			return
		}
		go s.handleMessage(conn, msg)
	}
}

func (s *Server) handleMessage(conn net.Conn, msg someip.Message) {
	if msg.MessageType == someip.RequestNoReturn {
		s.mu.RLock()
		handler, ok := s.handlers[msg.MethodID]
		s.mu.RUnlock()
		if ok {
			go func() { _, _ = handler(context.Background(), msg) }()
		}
		return
	}

	s.mu.RLock()
	handler, ok := s.handlers[msg.MethodID]
	s.mu.RUnlock()

	resp := someip.Message{
		ServiceID:        s.cfg.ServiceID,
		MethodID:         msg.MethodID,
		ClientID:         msg.ClientID,
		SessionID:        msg.SessionID,
		ProtocolVersion:  0x01,
		InterfaceVersion: s.cfg.InterfaceVersion,
	}

	if !ok {
		resp.MessageType = someip.Error
		resp.ReturnCode = someip.UnknownMethod
		_, _ = conn.Write(codec.Encode(nil, resp))
		return
	}

	payload, handlerErr := handler(context.Background(), msg)
	if handlerErr != nil {
		resp.MessageType = someip.Error
		resp.ReturnCode = someip.NotOK
	} else {
		resp.MessageType = someip.Response
		resp.ReturnCode = someip.OK
		resp.Payload = payload
	}
	_, _ = conn.Write(codec.Encode(nil, resp))
}

// ── Service ───────────────────────────────────────────────────────────────────

// ServiceConfig configures a TCP [Service].
type ServiceConfig struct {
	// ServerAddr is the TCP address of the remote server (e.g. "10.0.0.1:30509").
	ServerAddr string
	// ServiceID identifies the target service.
	ServiceID someip.ServiceID
	// InstanceID identifies the target instance.
	InstanceID someip.InstanceID
	// Timeout is the per-call deadline. Zero uses the default (5 s).
	Timeout time.Duration
	// DialTimeout is the connection establishment deadline. Zero uses 10 s.
	DialTimeout time.Duration
}

// Service is a SOME/IP TCP client.
// A Service is safe for concurrent use from multiple goroutines.
//
//fusa:req REQ-TCP-002
type Service struct {
	cfg     ServiceConfig
	timeout time.Duration

	mu        sync.Mutex
	conn      net.Conn
	sessionID uint16
	pending   map[someip.SessionID]chan someip.Message

	closed atomic.Bool
	wg     sync.WaitGroup
}

// NewService creates a SOME/IP TCP client and connects to the remote server.
func NewService(cfg ServiceConfig) (*Service, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = defaultDialTimeout
	}

	svc := &Service{
		cfg:     cfg,
		timeout: timeout,
		pending: make(map[someip.SessionID]chan someip.Message),
	}

	conn, err := net.DialTimeout("tcp4", cfg.ServerAddr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("tcp: dial %q: %w", cfg.ServerAddr, err)
	}
	svc.conn = conn

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
//
//fusa:req REQ-TCP-003
func (svc *Service) Call(ctx context.Context, methodID someip.MethodID, payload []byte) (someip.Message, error) {
	if svc.closed.Load() {
		return someip.Message{}, someip.ErrClosed
	}

	sessionID := svc.nextSession()
	ch := make(chan someip.Message, 1)

	svc.mu.Lock()
	conn := svc.conn
	svc.pending[sessionID] = ch
	svc.mu.Unlock()

	defer func() {
		svc.mu.Lock()
		delete(svc.pending, sessionID)
		svc.mu.Unlock()
	}()

	if conn == nil {
		return someip.Message{}, someip.ErrNotReady
	}

	req := someip.Message{
		ServiceID:   svc.cfg.ServiceID,
		MethodID:    methodID,
		SessionID:   sessionID,
		ClientID:    0x0001,
		MessageType: someip.Request,
		ReturnCode:  someip.OK,
		Payload:     payload,
	}
	if _, err := conn.Write(codec.Encode(nil, req)); err != nil {
		return someip.Message{}, fmt.Errorf("tcp: send: %w", err)
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
func (svc *Service) CallNoReturn(_ context.Context, methodID someip.MethodID, payload []byte) error {
	if svc.closed.Load() {
		return someip.ErrClosed
	}
	svc.mu.Lock()
	conn := svc.conn
	svc.mu.Unlock()
	if conn == nil {
		return someip.ErrNotReady
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
	_, err := conn.Write(codec.Encode(nil, req))
	return err
}

// Subscribe is not supported directly over TCP (use SD + UDP for event subscriptions).
// Returns [someip.ErrNotReady].
func (svc *Service) Subscribe(_ someip.EventID, _ ...someip.SubscribeOption) (someip.Subscription, error) {
	return nil, someip.ErrNotReady
}

// Close disconnects the service.
func (svc *Service) Close() error {
	if !svc.closed.CompareAndSwap(false, true) {
		return nil
	}
	svc.mu.Lock()
	conn := svc.conn
	svc.mu.Unlock()
	var err error
	if conn != nil {
		err = conn.Close()
	}
	svc.wg.Wait()
	return err
}

func (svc *Service) readLoop() {
	defer svc.wg.Done()
	svc.mu.Lock()
	conn := svc.conn
	svc.mu.Unlock()
	if conn == nil {
		return
	}
	for {
		msg, err := readFrame(conn)
		if err != nil {
			// Connection lost — pending callers will hit their deadline.
			return
		}

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

// ── framing ───────────────────────────────────────────────────────────────────

// readFrame reads exactly one SOME/IP frame from conn using the Length field.
func readFrame(r io.Reader) (someip.Message, error) {
	hdr := make([]byte, codec.HeaderSize)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return someip.Message{}, err
	}
	// Length field (bytes 4-7) = 8 + payload length.
	length := binary.BigEndian.Uint32(hdr[4:8])
	if length < 8 {
		return someip.Message{}, someip.ErrMalformedMessage
	}
	payloadLen := length - 8
	frame := make([]byte, codec.HeaderSize+payloadLen)
	copy(frame, hdr)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, frame[codec.HeaderSize:]); err != nil {
			return someip.Message{}, err
		}
	}
	return codec.Decode(frame)
}
