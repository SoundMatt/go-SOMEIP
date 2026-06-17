// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package mock provides an in-process SOME/IP transport with no network
// dependency. It is the recommended implementation for unit tests and
// development workflows that do not require real automotive Ethernet.
//
// A mock [Server] and [Service] share a common [Bus]. The Bus routes method
// calls and event notifications synchronously within the same process.
//
// Usage:
//
//	bus := mock.NewBus()
//
//	srv, _ := bus.NewServer(someip.ServiceID(0x1234), someip.InstanceID(0x0001))
//	srv.RegisterMethod(someip.MethodID(0x0001), func(ctx context.Context, req someip.Message) ([]byte, error) {
//	    return []byte("pong"), nil
//	})
//
//	svc, _ := bus.NewService(someip.ServiceID(0x1234), someip.InstanceID(0x0001))
//	resp, _ := svc.Call(context.Background(), someip.MethodID(0x0001), []byte("ping"))
package mock

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	someip "github.com/SoundMatt/go-SOMEIP"
)

const defaultChanDepth = 64

// Bus is an in-process routing layer that connects servers and services.
// A Bus is safe for concurrent use from multiple goroutines.
type Bus struct {
	mu      sync.RWMutex
	servers map[busKey]*server
}

type busKey struct {
	serviceID  someip.ServiceID
	instanceID someip.InstanceID
}

//fusa:req REQ-MOCK-001

// NewBus returns an empty in-process bus.
func NewBus() *Bus {
	return &Bus{servers: make(map[busKey]*server)}
}

//fusa:req REQ-MOCK-002

// NewServer creates a [someip.Server] attached to this bus.
// Returns [someip.ErrClosed] if the bus is closed.
func (b *Bus) NewServer(serviceID someip.ServiceID, instanceID someip.InstanceID) (someip.Server, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := busKey{serviceID, instanceID}
	s := &server{
		bus:        b,
		key:        key,
		handlers:   make(map[someip.MethodID]someip.MethodHandler),
		subs:       make(map[someip.EventID]map[*subscription]struct{}),
		sessionSeq: 1,
	}
	b.servers[key] = s
	return s, nil
}

//fusa:req REQ-MOCK-003

// NewService creates a [someip.Service] connected to an existing server on this bus.
// Returns [someip.ErrUnknownService] if no server is registered for the given
// service/instance pair.
func (b *Bus) NewService(serviceID someip.ServiceID, instanceID someip.InstanceID) (someip.Service, error) {
	b.mu.RLock()
	srv, ok := b.servers[busKey{serviceID, instanceID}]
	b.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: service 0x%04x/0x%04x not found on bus", someip.ErrUnknownService, serviceID, instanceID)
	}
	return &service{srv: srv}, nil
}

// ── server ────────────────────────────────────────────────────────────────────

type server struct {
	bus        *Bus
	key        busKey
	mu         sync.RWMutex
	handlers   map[someip.MethodID]someip.MethodHandler
	subs       map[someip.EventID]map[*subscription]struct{}
	sessionSeq uint16
	closed     atomic.Bool
}

//fusa:req REQ-MOCK-004
func (s *server) RegisterMethod(methodID someip.MethodID, handler someip.MethodHandler) error {
	if s.closed.Load() {
		return someip.ErrClosed
	}
	s.mu.Lock()
	s.handlers[methodID] = handler
	s.mu.Unlock()
	return nil
}

//fusa:req REQ-MOCK-005
func (s *server) Emit(eventID someip.EventID, payload []byte) error {
	if s.closed.Load() {
		return someip.ErrClosed
	}

	s.mu.RLock()
	subs := make([]*subscription, 0, len(s.subs[eventID]))
	for sub := range s.subs[eventID] {
		subs = append(subs, sub)
	}
	s.mu.RUnlock()

	msg := someip.Message{
		ServiceID:       s.key.serviceID,
		MethodID:        someip.MethodID(eventID),
		ProtocolVersion: 0x01,
		MessageType:     someip.MsgTypeNotification,
		ReturnCode:      someip.RetOK,
		Payload:         payload,
	}

	for _, sub := range subs {
		if sub.closed.Load() {
			continue
		}
		select {
		case sub.ch <- msg:
		default:
			// Drop if channel is full — non-blocking.
		}
	}
	return nil
}

//fusa:req REQ-MOCK-006
func (s *server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.bus.mu.Lock()
	delete(s.bus.servers, s.key)
	s.bus.mu.Unlock()

	// Close all subscriptions.
	s.mu.Lock()
	for _, subSet := range s.subs {
		for sub := range subSet {
			sub.closeOnce()
		}
	}
	s.mu.Unlock()
	return nil
}

// call is invoked by service.Call.
func (s *server) call(ctx context.Context, methodID someip.MethodID, clientID someip.ClientID, sessionID someip.SessionID, payload []byte) (someip.Message, error) {
	if s.closed.Load() {
		return someip.Message{}, someip.ErrClosed
	}

	s.mu.RLock()
	handler, ok := s.handlers[methodID]
	s.mu.RUnlock()
	if !ok {
		return someip.Message{}, fmt.Errorf("%w: method 0x%04x", someip.ErrUnknownMethod, methodID)
	}

	req := someip.Message{
		ServiceID:       s.key.serviceID,
		MethodID:        methodID,
		ClientID:        clientID,
		SessionID:       sessionID,
		ProtocolVersion: 0x01,
		MessageType:     someip.MsgTypeRequest,
		ReturnCode:      someip.RetOK,
		Payload:         payload,
	}

	respPayload, err := handler(ctx, req)
	if err != nil {
		return someip.Message{
			ServiceID:       s.key.serviceID,
			MethodID:        methodID,
			ClientID:        clientID,
			SessionID:       sessionID,
			ProtocolVersion: 0x01,
			MessageType:     someip.MsgTypeError,
			ReturnCode:      someip.RetNotOK,
		}, err
	}

	return someip.Message{
		ServiceID:       s.key.serviceID,
		MethodID:        methodID,
		ClientID:        clientID,
		SessionID:       sessionID,
		ProtocolVersion: 0x01,
		MessageType:     someip.MsgTypeResponse,
		ReturnCode:      someip.RetOK,
		Payload:         respPayload,
	}, nil
}

// addSub registers a subscription on the server.
func (s *server) addSub(eventID someip.EventID, sub *subscription) {
	s.mu.Lock()
	if s.subs[eventID] == nil {
		s.subs[eventID] = make(map[*subscription]struct{})
	}
	s.subs[eventID][sub] = struct{}{}
	s.mu.Unlock()
}

// removeSub deregisters a subscription.
func (s *server) removeSub(eventID someip.EventID, sub *subscription) {
	s.mu.Lock()
	delete(s.subs[eventID], sub)
	s.mu.Unlock()
}

// ── service ───────────────────────────────────────────────────────────────────

type service struct {
	srv       *server
	mu        sync.Mutex
	sessionID uint16
	closed    atomic.Bool
}

func (svc *service) nextSession() someip.SessionID {
	svc.mu.Lock()
	svc.sessionID++
	id := svc.sessionID
	svc.mu.Unlock()
	return someip.SessionID(id)
}

//fusa:req REQ-MOCK-007
func (svc *service) Call(ctx context.Context, methodID someip.MethodID, payload []byte) (someip.Message, error) {
	if svc.closed.Load() {
		return someip.Message{}, someip.ErrClosed
	}
	return svc.srv.call(ctx, methodID, 0x0001, svc.nextSession(), payload)
}

//fusa:req REQ-MOCK-008
func (svc *service) CallNoReturn(ctx context.Context, methodID someip.MethodID, payload []byte) error {
	if svc.closed.Load() {
		return someip.ErrClosed
	}
	if svc.srv.closed.Load() {
		return someip.ErrClosed
	}
	// Fire-and-forget: verify the method is registered but don't wait for response.
	svc.srv.mu.RLock()
	_, ok := svc.srv.handlers[methodID]
	svc.srv.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: method 0x%04x", someip.ErrUnknownMethod, methodID)
	}

	req := someip.Message{
		ServiceID:       svc.srv.key.serviceID,
		MethodID:        methodID,
		ClientID:        0x0001,
		SessionID:       svc.nextSession(),
		ProtocolVersion: 0x01,
		MessageType:     someip.MsgTypeRequestNoReturn,
		ReturnCode:      someip.RetOK,
		Payload:         payload,
	}

	svc.srv.mu.RLock()
	handler := svc.srv.handlers[methodID]
	svc.srv.mu.RUnlock()

	// Invoke in goroutine — caller does not wait.
	go func() { _, _ = handler(ctx, req) }()
	return nil
}

//fusa:req REQ-MOCK-009
func (svc *service) Subscribe(eventID someip.EventID, opts ...someip.SubscriberOption) (someip.Subscription, error) {
	if svc.closed.Load() {
		return nil, someip.ErrClosed
	}
	cfg := someip.ApplySubscriberOpts(opts)
	depth := cfg.ChanDepth(defaultChanDepth)

	sub := &subscription{
		srv:     svc.srv,
		eventID: eventID,
		ch:      make(chan someip.Message, depth),
	}
	svc.srv.addSub(eventID, sub)
	return sub, nil
}

//fusa:req REQ-MOCK-010
func (svc *service) Close() error {
	svc.closed.Store(true)
	return nil
}

// ── subscription ──────────────────────────────────────────────────────────────

type subscription struct {
	srv     *server
	eventID someip.EventID
	ch      chan someip.Message
	once    sync.Once
	closed  atomic.Bool
}

//fusa:req REQ-MOCK-011
func (sub *subscription) C() <-chan someip.Message {
	return sub.ch
}

//fusa:req REQ-MOCK-012
func (sub *subscription) Unsubscribe() error {
	sub.srv.removeSub(sub.eventID, sub)
	return nil
}

//fusa:req REQ-MOCK-013
func (sub *subscription) Close() error {
	_ = sub.Unsubscribe()
	sub.closeOnce()
	return nil
}

func (sub *subscription) closeOnce() {
	sub.once.Do(func() {
		sub.closed.Store(true)
		close(sub.ch)
	})
}
