// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration

package udp_test

import (
	"bytes"
	"context"
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
