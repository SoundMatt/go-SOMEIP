// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
)

// header builds a bare 16-byte SOME/IP header with only the Length field
// (bytes 4-7) populated, sufficient to exercise readFrame's framing logic
// without a real connection.
func header(length uint32) []byte {
	h := make([]byte, codec.HeaderSize)
	binary.BigEndian.PutUint32(h[4:8], length)
	return h
}

// TestReadFrame_LengthOverflowWraps is a regression test for go-SOMEIP-02: a
// Length field of 0xFFFFFFFF made `codec.HeaderSize + payloadLen` wrap to 7
// in uint32 arithmetic, so `make([]byte, 7)` was allocated and the following
// `frame[codec.HeaderSize:]` slice panicked with "slice bounds out of range"
// — a single 16-byte attacker-controlled header crashing the server process.
// readFrame must reject the frame before any allocation and must never panic.
func TestReadFrame_LengthOverflowWraps(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("readFrame panicked: %v", r)
		}
	}()
	_, err := readFrame(bytes.NewReader(header(0xFFFFFFFF)))
	if !errors.Is(err, someip.ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

// TestReadFrame_LargeNonWrappingLength is a regression test for
// go-SOMEIP-02: a large but non-wrapping Length field (~1 GiB) forced a
// multi-hundred-MB allocation per connection from 16 attacker bytes.
// readFrame must reject it via maxFrameSize before allocating.
func TestReadFrame_LargeNonWrappingLength(t *testing.T) {
	_, err := readFrame(bytes.NewReader(header(0x40000000)))
	if !errors.Is(err, someip.ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

// TestReadFrame_ShortLength verifies the length<8 malformed-header path is
// still rejected cleanly (not part of the overflow bug, but exercises the
// same widened-arithmetic code path).
func TestReadFrame_ShortLength(t *testing.T) {
	_, err := readFrame(bytes.NewReader(header(3)))
	if !errors.Is(err, someip.ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage", err)
	}
}

// TestReadFrame_ValidRoundTrip verifies a well-formed frame is still parsed
// correctly (the overflow-safety fix must not reject legitimate frames).
func TestReadFrame_ValidRoundTrip(t *testing.T) {
	msg := someip.Message{
		ServiceID:        0x1234,
		MethodID:         0x0001,
		ClientID:         0x0001,
		SessionID:        0x0001,
		ProtocolVersion:  codec.ProtocolVersion,
		InterfaceVersion: 1,
		MessageType:      someip.MsgTypeRequest,
		ReturnCode:       someip.RetOK,
		Payload:          []byte("hello"),
	}
	frame := codec.Encode(nil, msg)
	got, err := readFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if string(got.Payload) != "hello" {
		t.Errorf("payload = %q, want %q", got.Payload, "hello")
	}
}

// TestHandleMessage_WrongInterfaceVersionRejected is a regression test for
// go-SOMEIP-08: AUTOSAR SOME/IP Protocol Specification Table 27 mandates
// RetWrongInterfaceVersion (0x08) when a request's InterfaceVersion does not
// match the served interface. handleMessage used to skip this check
// entirely and dispatch mismatched-interface requests straight to the
// handler.
func TestHandleMessage_WrongInterfaceVersionRejected(t *testing.T) {
	s := &Server{
		cfg: ServerConfig{
			ServiceID:        0x1234,
			InstanceID:       0x0001,
			InterfaceVersion: 2,
		},
		handlers: make(map[someip.MethodID]someip.MethodHandler),
	}
	called := false
	_ = s.RegisterMethod(0x0001, func(_ context.Context, _ someip.Message) ([]byte, error) {
		called = true
		return nil, nil
	})

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := someip.Message{
		ServiceID:        0x1234,
		MethodID:         0x0001,
		SessionID:        0x0001,
		ProtocolVersion:  codec.ProtocolVersion,
		InterfaceVersion: 1, // mismatched: server is configured for version 2
		MessageType:      someip.MsgTypeRequest,
	}

	respCh := make(chan someip.Message, 1)
	errCh := make(chan error, 1)
	go func() {
		frame := make([]byte, codec.HeaderSize)
		if _, err := io.ReadFull(client, frame); err != nil {
			errCh <- err
			return
		}
		msg, err := codec.Decode(frame)
		if err != nil {
			errCh <- err
			return
		}
		respCh <- msg
	}()

	s.handleMessage(server, req)

	select {
	case resp := <-respCh:
		if resp.MessageType != someip.MsgTypeError {
			t.Errorf("MessageType = 0x%02x, want MsgTypeError", resp.MessageType)
		}
		if resp.ReturnCode != someip.RetWrongInterfaceVersion {
			t.Errorf("ReturnCode = 0x%02x, want RetWrongInterfaceVersion (0x08)", resp.ReturnCode)
		}
	case err := <-errCh:
		t.Fatalf("reading response: %v", err)
	}

	if called {
		t.Error("handler was invoked for a mismatched InterfaceVersion request")
	}
}
