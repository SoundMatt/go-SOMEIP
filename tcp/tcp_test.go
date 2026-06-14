// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration

package tcp_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/tcp"
)

const (
	testServiceID  someip.ServiceID = 0x1234
	testInstanceID someip.InstanceID = 0x0001
	testMethodEcho someip.MethodID  = 0x0001
	testMethodErr  someip.MethodID  = 0x0002
	testMethodNoRet someip.MethodID = 0x0003
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func startServer(t *testing.T, addr string) *tcp.Server {
	t.Helper()
	srv, err := tcp.NewServer(tcp.ServerConfig{
		Addr:       addr,
		ServiceID:  testServiceID,
		InstanceID: testInstanceID,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.RegisterMethod(testMethodEcho, func(_ context.Context, req someip.Message) ([]byte, error) {
		return req.Payload, nil
	}); err != nil {
		t.Fatalf("RegisterMethod echo: %v", err)
	}
	if err := srv.RegisterMethod(testMethodErr, func(_ context.Context, _ someip.Message) ([]byte, error) {
		return nil, errors.New("handler error")
	}); err != nil {
		t.Fatalf("RegisterMethod err: %v", err)
	}
	if err := srv.RegisterMethod(testMethodNoRet, func(_ context.Context, _ someip.Message) ([]byte, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("RegisterMethod noreturner: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

func dialService(t *testing.T, addr string) *tcp.Service {
	t.Helper()
	svc, err := tcp.NewService(tcp.ServiceConfig{
		ServerAddr: addr,
		ServiceID:  testServiceID,
		InstanceID: testInstanceID,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc
}

// TestEchoRoundTrip verifies a basic Call → handler → response cycle over TCP.
func TestEchoRoundTrip(t *testing.T) {
	//fusa:test REQ-TCP-001
	//fusa:test REQ-TCP-002
	//fusa:test REQ-TCP-003
	//fusa:test REQ-TCP-008
	//fusa:test REQ-TCP-009
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	want := []byte("hello-tcp")
	resp, err := svc.Call(context.Background(), testMethodEcho, want)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(resp.Payload) != string(want) {
		t.Errorf("payload = %q, want %q", resp.Payload, want)
	}
	if resp.MessageType != someip.Response {
		t.Errorf("MessageType = %v, want Response", resp.MessageType)
	}
}

// TestHandlerError verifies the server returns an Error message when the handler fails.
func TestHandlerError(t *testing.T) {
	//fusa:test REQ-TCP-005
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	resp, err := svc.Call(context.Background(), testMethodErr, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.MessageType != someip.Error {
		t.Errorf("MessageType = %v, want Error", resp.MessageType)
	}
}

// TestUnknownMethod verifies the server returns an Error for unregistered methods.
func TestUnknownMethod(t *testing.T) {
	//fusa:test REQ-TCP-004
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	resp, err := svc.Call(context.Background(), someip.MethodID(0xDEAD), nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.MessageType != someip.Error {
		t.Errorf("MessageType = %v, want Error", resp.MessageType)
	}
	if resp.ReturnCode != someip.UnknownMethod {
		t.Errorf("ReturnCode = %v, want UnknownMethod", resp.ReturnCode)
	}
}

// TestConcurrentCalls verifies that concurrent calls are demuxed correctly by SessionID.
func TestConcurrentCalls(t *testing.T) {
	//fusa:test REQ-TCP-009
	addr := freeAddr(t)
	srv, err := tcp.NewServer(tcp.ServerConfig{Addr: addr, ServiceID: testServiceID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	// Handler echoes back a tagged payload so we can verify per-call correctness.
	if err := srv.RegisterMethod(testMethodEcho, func(_ context.Context, req someip.Message) ([]byte, error) {
		// Simulate variable latency to exercise interleaving.
		time.Sleep(time.Duration(req.Payload[0]%5) * time.Millisecond)
		return req.Payload, nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := dialService(t, addr)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			payload := []byte(fmt.Sprintf("call-%02d", i))
			resp, err := svc.Call(context.Background(), testMethodEcho, payload)
			if err != nil {
				errs[i] = err
				return
			}
			if string(resp.Payload) != string(payload) {
				errs[i] = fmt.Errorf("call %d: got %q, want %q", i, resp.Payload, payload)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d failed: %v", i, err)
		}
	}
}

// TestCallNoReturn sends fire-and-forget and verifies no error.
func TestCallNoReturn(t *testing.T) {
	//fusa:test REQ-TCP-012
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	if err := svc.CallNoReturn(context.Background(), testMethodNoRet, []byte("fire")); err != nil {
		t.Fatalf("CallNoReturn: %v", err)
	}
}

// TestContextCancel verifies that a cancelled context aborts the call.
func TestContextCancel(t *testing.T) {
	//fusa:test REQ-TCP-010
	addr := freeAddr(t)
	srv, err := tcp.NewServer(tcp.ServerConfig{Addr: addr, ServiceID: testServiceID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	// Handler blocks until context is cancelled.
	if err := srv.RegisterMethod(testMethodEcho, func(ctx context.Context, _ someip.Message) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	svc := dialService(t, addr)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = svc.Call(ctx, testMethodEcho, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestTimeout verifies the per-call timeout fires when the server is slow.
func TestTimeout(t *testing.T) {
	//fusa:test REQ-TCP-011
	addr := freeAddr(t)
	srv, err := tcp.NewServer(tcp.ServerConfig{Addr: addr, ServiceID: testServiceID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	// Handler blocks forever.
	if err := srv.RegisterMethod(testMethodEcho, func(_ context.Context, _ someip.Message) ([]byte, error) {
		time.Sleep(10 * time.Second)
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := tcp.NewService(tcp.ServiceConfig{
		ServerAddr: addr,
		ServiceID:  testServiceID,
		Timeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	_, err = svc.Call(context.Background(), testMethodEcho, nil)
	if !errors.Is(err, someip.ErrTimeout) {
		t.Errorf("err = %v, want ErrTimeout", err)
	}
}

// TestServerClose verifies Close stops the listener and causes new dials to fail.
func TestServerClose(t *testing.T) {
	//fusa:test REQ-TCP-007
	addr := freeAddr(t)
	srv, err := tcp.NewServer(tcp.ServerConfig{Addr: addr, ServiceID: testServiceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second close is a no-op.
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	// Dial must now fail.
	_, err = tcp.NewService(tcp.ServiceConfig{
		ServerAddr:  addr,
		ServiceID:   testServiceID,
		DialTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Error("expected dial error after server closed, got nil")
	}
}

// TestServiceClosedCall verifies Call returns ErrClosed after Close.
func TestServiceClosedCall(t *testing.T) {
	//fusa:test REQ-TCP-014
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := svc.Call(context.Background(), testMethodEcho, nil)
	if !errors.Is(err, someip.ErrClosed) {
		t.Errorf("err = %v, want ErrClosed", err)
	}
}

// TestEmitNoOp verifies Server.Emit returns nil (TCP has no broadcast path).
func TestEmitNoOp(t *testing.T) {
	//fusa:test REQ-TCP-006
	addr := freeAddr(t)
	srv := startServer(t, addr)
	if err := srv.Emit(someip.EventID(0x8001), []byte("event")); err != nil {
		t.Errorf("Emit: %v", err)
	}
}

// TestSubscribeNotSupported verifies Service.Subscribe returns ErrNotReady.
func TestSubscribeNotSupported(t *testing.T) {
	//fusa:test REQ-TCP-013
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	_, err := svc.Subscribe(someip.EventID(0x8001))
	if !errors.Is(err, someip.ErrNotReady) {
		t.Errorf("Subscribe err = %v, want ErrNotReady", err)
	}
}

// TestLargePayload verifies round-trip of a payload larger than a typical UDP MTU.
func TestLargePayload(t *testing.T) {
	//fusa:test REQ-TCP-003
	//fusa:test REQ-TCP-009
	addr := freeAddr(t)
	startServer(t, addr)
	svc := dialService(t, addr)

	payload := make([]byte, 64*1024) // 64 KB
	for i := range payload {
		payload[i] = byte(i)
	}
	resp, err := svc.Call(context.Background(), testMethodEcho, payload)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(resp.Payload) != len(payload) {
		t.Errorf("payload len = %d, want %d", len(resp.Payload), len(payload))
	}
	for i, b := range resp.Payload {
		if b != payload[i] {
			t.Errorf("payload[%d] = %d, want %d", i, b, payload[i])
			break
		}
	}
}
