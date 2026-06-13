// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package mock_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/mock"
)

func newBusWithEchoServer(t *testing.T) (*mock.Bus, someip.Server) {
	t.Helper()
	bus := mock.NewBus()
	srv, err := bus.NewServer(0x1234, 0x0001)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	_ = srv.RegisterMethod(0x0001, func(_ context.Context, req someip.Message) ([]byte, error) {
		return req.Payload, nil
	})
	return bus, srv
}

func TestCallEcho(t *testing.T) {
	bus, srv := newBusWithEchoServer(t)
	defer srv.Close()

	svc, err := bus.NewService(0x1234, 0x0001)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	resp, err := svc.Call(context.Background(), 0x0001, []byte("hello"))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.MessageType != someip.Response {
		t.Errorf("MessageType: got 0x%02x, want Response", resp.MessageType)
	}
	if !bytes.Equal(resp.Payload, []byte("hello")) {
		t.Errorf("Payload: got %q, want %q", resp.Payload, "hello")
	}
}

func TestCallUnknownMethod(t *testing.T) {
	bus, srv := newBusWithEchoServer(t)
	defer srv.Close()

	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	_, err := svc.Call(context.Background(), 0x9999, nil)
	if !errors.Is(err, someip.ErrUnknownMethod) {
		t.Errorf("expected ErrUnknownMethod, got %v", err)
	}
}

func TestNewServiceUnknownService(t *testing.T) {
	bus := mock.NewBus()
	_, err := bus.NewService(0xDEAD, 0x0001)
	if !errors.Is(err, someip.ErrUnknownService) {
		t.Errorf("expected ErrUnknownService, got %v", err)
	}
}

func TestCallNoReturn(t *testing.T) {
	bus := mock.NewBus()
	srv, _ := bus.NewServer(0x1234, 0x0001)
	defer srv.Close()

	called := make(chan struct{}, 1)
	_ = srv.RegisterMethod(0x0002, func(_ context.Context, _ someip.Message) ([]byte, error) {
		called <- struct{}{}
		return nil, nil
	})

	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	if err := svc.CallNoReturn(context.Background(), 0x0002, []byte("fire")); err != nil {
		t.Fatalf("CallNoReturn: %v", err)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("handler not called within 1 s")
	}
}

func TestEventSubscribeEmit(t *testing.T) {
	bus, srv := newBusWithEchoServer(t)
	defer srv.Close()

	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	const eventID someip.EventID = 0x8001
	sub, err := svc.Subscribe(eventID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	want := []byte("event-payload")
	if err := srv.Emit(eventID, want); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	select {
	case msg := <-sub.C():
		if !bytes.Equal(msg.Payload, want) {
			t.Errorf("event payload: got %q, want %q", msg.Payload, want)
		}
		if msg.MessageType != someip.Notification {
			t.Errorf("MessageType: got 0x%02x, want Notification", msg.MessageType)
		}
	case <-time.After(time.Second):
		t.Fatal("event not received within 1 s")
	}
}

func TestSubscribeUnsubscribeNoFurtherEvents(t *testing.T) {
	bus, srv := newBusWithEchoServer(t)
	defer srv.Close()

	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	const eventID someip.EventID = 0x8001
	sub, _ := svc.Subscribe(eventID)

	_ = sub.Unsubscribe()
	_ = srv.Emit(eventID, []byte("after-unsubscribe"))

	select {
	case msg, ok := <-sub.C():
		if ok {
			t.Errorf("received event after Unsubscribe: %v", msg)
		}
	case <-time.After(50 * time.Millisecond):
		// expected — no event delivered
	}
}

func TestServerCloseIdempotent(t *testing.T) {
	bus := mock.NewBus()
	srv, _ := bus.NewServer(0x1234, 0x0001)
	if err := srv.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCallAfterServerClose(t *testing.T) {
	bus, srv := newBusWithEchoServer(t)
	svc, _ := bus.NewService(0x1234, 0x0001)
	srv.Close()

	_, err := svc.Call(context.Background(), 0x0001, nil)
	if !errors.Is(err, someip.ErrClosed) {
		t.Errorf("expected ErrClosed after server close, got %v", err)
	}
}

func TestSessionIDIncrement(t *testing.T) {
	bus, srv := newBusWithEchoServer(t)
	defer srv.Close()

	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	var sessions []someip.SessionID
	_ = srv.RegisterMethod(0x0001, func(_ context.Context, req someip.Message) ([]byte, error) {
		sessions = append(sessions, req.SessionID)
		return req.Payload, nil
	})

	for i := 0; i < 3; i++ {
		_, _ = svc.Call(context.Background(), 0x0001, nil)
	}
	for i := 1; i < len(sessions); i++ {
		if sessions[i] <= sessions[i-1] {
			t.Errorf("session IDs not monotonically increasing: %v", sessions)
		}
	}
}

// BenchmarkCall measures synchronous method call overhead.
func BenchmarkCall(b *testing.B) {
	bus := mock.NewBus()
	srv, _ := bus.NewServer(0x1234, 0x0001)
	defer srv.Close()
	_ = srv.RegisterMethod(0x0001, func(_ context.Context, req someip.Message) ([]byte, error) {
		return req.Payload, nil
	})
	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	payload := []byte("bench-payload")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Call(context.Background(), 0x0001, payload)
	}
}

// BenchmarkEmit measures event fan-out overhead with a single subscriber.
func BenchmarkEmit(b *testing.B) {
	bus := mock.NewBus()
	srv, _ := bus.NewServer(0x1234, 0x0001)
	defer srv.Close()
	svc, _ := bus.NewService(0x1234, 0x0001)
	defer svc.Close()

	const eventID someip.EventID = 0x8001
	sub, _ := svc.Subscribe(eventID, someip.WithChannelDepth(b.N+1))
	defer sub.Close()

	payload := []byte("bench-event")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = srv.Emit(eventID, payload)
	}
}
