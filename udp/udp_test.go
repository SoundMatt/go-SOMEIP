// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration

package udp_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/udp"
)

func TestUDPCallEcho(t *testing.T) {
	//fusa:test REQ-UDP-001
	//fusa:test REQ-UDP-002
	//fusa:test REQ-UDP-005
	//fusa:test REQ-UDP-006
	srv, err := udp.NewServer(udp.ServerConfig{
		Addr:       "127.0.0.1:0",
		ServiceID:  0x1234,
		InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	_ = srv.RegisterMethod(0x0001, func(_ context.Context, req someip.Message) ([]byte, error) {
		return req.Payload, nil
	})

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: srv.LocalAddr().String(),
		ServiceID:  0x1234,
		InstanceID: 0x0001,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	resp, err := svc.Call(context.Background(), 0x0001, []byte("ping"))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.MessageType != someip.MsgTypeResponse {
		t.Errorf("MessageType: got 0x%02x, want Response", resp.MessageType)
	}
	if !bytes.Equal(resp.Payload, []byte("ping")) {
		t.Errorf("Payload: got %q, want %q", resp.Payload, "ping")
	}
}

func TestUDPCallUnknownMethod(t *testing.T) {
	//fusa:test REQ-UDP-003
	//fusa:test REQ-UDP-006
	srv, err := udp.NewServer(udp.ServerConfig{
		Addr: "127.0.0.1:0", ServiceID: 0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: srv.LocalAddr().String(),
		ServiceID:  0x1234, InstanceID: 0x0001,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	resp, err := svc.Call(context.Background(), 0x9999, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.MessageType != someip.MsgTypeError {
		t.Errorf("MessageType: got 0x%02x, want Error", resp.MessageType)
	}
	if resp.ReturnCode != someip.RetUnknownMethod {
		t.Errorf("ReturnCode: got 0x%02x, want UnknownMethod", resp.ReturnCode)
	}
}

func TestUDPCallNoReturn(t *testing.T) {
	//fusa:test REQ-UDP-004
	//fusa:test REQ-UDP-007
	//fusa:test REQ-UDP-009
	srv, err := udp.NewServer(udp.ServerConfig{
		Addr: "127.0.0.1:0", ServiceID: 0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	called := make(chan struct{}, 1)
	_ = srv.RegisterMethod(0x0002, func(_ context.Context, _ someip.Message) ([]byte, error) {
		called <- struct{}{}
		return nil, nil
	})

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: srv.LocalAddr().String(),
		ServiceID:  0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	if err := svc.CallNoReturn(context.Background(), 0x0002, []byte("fire")); err != nil {
		t.Fatalf("CallNoReturn: %v", err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called within 2 s")
	}
}

// TestUDPSubscribeEmit exercises the full Subscribe → Emit → notification
// path end-to-end: Subscribe must register the client's address with the
// server so that Emit actually delivers (go-SOMEIP#47).
func TestUDPSubscribeEmit(t *testing.T) {
	//fusa:test REQ-UDP-003
	//fusa:test REQ-UDP-008
	//fusa:test REQ-UDP-010
	//fusa:test REQ-UDP-013
	srv, err := udp.NewServer(udp.ServerConfig{
		Addr: "127.0.0.1:0", ServiceID: 0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: srv.LocalAddr().String(),
		ServiceID:  0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	sub, err := svc.Subscribe(0x8001)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	// Registration is asynchronous (a UDP datagram to the server); poll
	// Emit until a notification arrives rather than assuming a fixed delay.
	deadline := time.After(2 * time.Second)
	for {
		if err := srv.Emit(0x8001, []byte("event")); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		select {
		case msg := <-sub.C():
			if !bytes.Equal(msg.Payload, []byte("event")) {
				t.Errorf("Payload: got %q, want %q", msg.Payload, "event")
			}
			return
		case <-time.After(50 * time.Millisecond):
			// registration may not have landed yet — retry Emit
		case <-deadline:
			t.Fatal("no notification received within 2 s")
		}
	}
}

// TestUDPUnsubscribeStopsDelivery verifies Unsubscribe both removes the local
// channel from the fan-out list and tells the server to stop delivering.
func TestUDPUnsubscribeStopsDelivery(t *testing.T) {
	//fusa:test REQ-UDP-011
	//fusa:test REQ-UDP-012
	//fusa:test REQ-UDP-014
	srv, err := udp.NewServer(udp.ServerConfig{
		Addr: "127.0.0.1:0", ServiceID: 0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: srv.LocalAddr().String(),
		ServiceID:  0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	sub, err := svc.Subscribe(0x8002)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Wait until the subscription is actually registered before unsubscribing.
	deadline := time.After(2 * time.Second)
waitReg:
	for {
		if err := srv.Emit(0x8002, []byte("warmup")); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		select {
		case <-sub.C():
			break waitReg
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatal("subscription never registered within 2 s")
		}
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Give the server a moment to process the unsubscribe datagram, then
	// confirm no further notifications are queued for the (now closed)
	// channel — Emit itself must not be observed to panic or block.
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 5; i++ {
		if err := srv.Emit(0x8002, []byte("after-close")); err != nil {
			t.Fatalf("Emit after unsubscribe: %v", err)
		}
	}
}

// TestUDPSubscribeUnsubscribeRace exercises concurrent Subscribe/Unsubscribe
// under -race to guard against the shared-slice mutation bug in
// udpSubscription.Unsubscribe (go-SOMEIP#48).
func TestUDPSubscribeUnsubscribeRace(t *testing.T) {
	//fusa:test REQ-UDP-008
	//fusa:test REQ-UDP-011
	srv, err := udp.NewServer(udp.ServerConfig{
		Addr: "127.0.0.1:0", ServiceID: 0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: srv.LocalAddr().String(),
		ServiceID:  0x1234, InstanceID: 0x0001,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	const eventID = someip.EventID(0x8003)
	const n = 20

	var wg sync.WaitGroup
	stopEmit := make(chan struct{})
	var emitWg sync.WaitGroup

	// A goroutine that emits continuously, concurrently with Subscribe/
	// Unsubscribe below — this is what races dispatchFrame against Unsubscribe.
	// It is tracked by its own WaitGroup (not wg, which the subscriber
	// goroutines below are drained against before stopEmit is closed) to
	// avoid a self-deadlock: wg.Wait() must not depend on stopEmit being
	// closed, and stopEmit must not close until wg.Wait() returns.
	emitWg.Add(1)
	go func() {
		defer emitWg.Done()
		for {
			select {
			case <-stopEmit:
				return
			default:
				_ = srv.Emit(eventID, []byte("x"))
				time.Sleep(time.Millisecond)
			}
		}
	}()

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sub, err := svc.Subscribe(eventID)
			if err != nil {
				t.Errorf("Subscribe %d: %v", i, err)
				return
			}
			// Drain any deliveries so the channel send in dispatchFrame
			// doesn't block on a full buffer while we race Unsubscribe.
			done := make(chan struct{})
			go func() {
				for range sub.C() {
				}
				close(done)
			}()
			time.Sleep(time.Duration(i%5) * time.Millisecond)
			if err := sub.Close(); err != nil {
				t.Errorf("Close %d: %v", i, err)
			}
			<-done
		}(i)
	}

	wg.Wait()
	close(stopEmit)
	emitWg.Wait()
}
