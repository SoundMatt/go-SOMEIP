// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package codec_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
)

// knownGoodFrame is a hand-crafted SOME/IP Request frame with payload "ping".
// ServiceID=0x1234, MethodID=0x0001, ClientID=0x0005, SessionID=0x0001,
// ProtoVer=0x01, IfaceVer=0x01, MsgType=Request(0x00), RetCode=OK(0x00),
// Payload="ping" (4 bytes) → Length=8+4=12.
var knownGoodFrame = []byte{
	0x12, 0x34, // ServiceID
	0x00, 0x01, // MethodID
	0x00, 0x00, 0x00, 0x0c, // Length = 12
	0x00, 0x05, // ClientID
	0x00, 0x01, // SessionID
	0x01,               // ProtocolVersion
	0x01,               // InterfaceVersion
	0x00,               // MessageType = Request
	0x00,               // ReturnCode = OK
	'p', 'i', 'n', 'g', // Payload
}

func TestDecodeKnownGoodFrame(t *testing.T) {
	//fusa:test REQ-CODEC-002
	msg, err := codec.Decode(knownGoodFrame)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.ServiceID != 0x1234 {
		t.Errorf("ServiceID: got 0x%04x, want 0x1234", msg.ServiceID)
	}
	if msg.MethodID != 0x0001 {
		t.Errorf("MethodID: got 0x%04x, want 0x0001", msg.MethodID)
	}
	if msg.ClientID != 0x0005 {
		t.Errorf("ClientID: got 0x%04x, want 0x0005", msg.ClientID)
	}
	if msg.SessionID != 0x0001 {
		t.Errorf("SessionID: got 0x%04x, want 0x0001", msg.SessionID)
	}
	if msg.ProtocolVersion != 0x01 {
		t.Errorf("ProtocolVersion: got 0x%02x, want 0x01", msg.ProtocolVersion)
	}
	if msg.InterfaceVersion != 0x01 {
		t.Errorf("InterfaceVersion: got 0x%02x, want 0x01", msg.InterfaceVersion)
	}
	if msg.MessageType != someip.MsgTypeRequest {
		t.Errorf("MessageType: got 0x%02x, want Request", msg.MessageType)
	}
	if msg.ReturnCode != someip.RetOK {
		t.Errorf("ReturnCode: got 0x%02x, want OK", msg.ReturnCode)
	}
	if !bytes.Equal(msg.Payload, []byte("ping")) {
		t.Errorf("Payload: got %q, want %q", msg.Payload, "ping")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	//fusa:test REQ-CODEC-001
	//fusa:test REQ-CODEC-002
	original := someip.Message{
		ServiceID:        0xABCD,
		MethodID:         0x8001,
		ClientID:         0x0042,
		SessionID:        0x00FF,
		ProtocolVersion:  0x01,
		InterfaceVersion: 0x02,
		MessageType:      someip.MsgTypeNotification,
		ReturnCode:       someip.RetOK,
		Payload:          []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}

	frame := codec.Encode(nil, original)
	decoded, err := codec.Decode(frame)
	if err != nil {
		t.Fatalf("Decode after Encode: %v", err)
	}

	if decoded.ServiceID != original.ServiceID {
		t.Errorf("ServiceID mismatch")
	}
	if decoded.MethodID != original.MethodID {
		t.Errorf("MethodID mismatch")
	}
	if decoded.MessageType != original.MessageType {
		t.Errorf("MessageType mismatch")
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload mismatch")
	}
}

func TestEncodeEmptyPayload(t *testing.T) {
	//fusa:test REQ-CODEC-001
	msg := someip.Message{
		ServiceID:   0x0001,
		MethodID:    0x0001,
		MessageType: someip.MsgTypeRequestNoReturn,
		ReturnCode:  someip.RetOK,
	}
	frame := codec.Encode(nil, msg)
	if len(frame) != codec.HeaderSize {
		t.Errorf("empty payload frame: got %d bytes, want %d", len(frame), codec.HeaderSize)
	}
	decoded, err := codec.Decode(frame)
	if err != nil {
		t.Fatalf("Decode empty: %v", err)
	}
	if len(decoded.Payload) != 0 {
		t.Errorf("payload not empty: %v", decoded.Payload)
	}
}

func TestDecodeShortFrame(t *testing.T) {
	//fusa:test REQ-CODEC-002
	_, err := codec.Decode([]byte{0x12, 0x34})
	if !errors.Is(err, codec.ErrShortFrame) {
		t.Errorf("expected ErrShortFrame, got %v", err)
	}
}

func TestDecodeLengthMismatch(t *testing.T) {
	//fusa:test REQ-CODEC-002
	// Valid header but Length field claims more bytes than present.
	b := make([]byte, codec.HeaderSize)
	b[4], b[5], b[6], b[7] = 0x00, 0x00, 0x00, 0x10 // Length = 16 → expects 24-byte frame
	_, err := codec.Decode(b)
	if !errors.Is(err, codec.ErrLengthMismatch) {
		t.Errorf("expected ErrLengthMismatch, got %v", err)
	}
}

// TestDecodeLengthNearUint32Wraparound is a regression test for
// go-SOMEIP-04: `wantTotal := int(8 + length)` computed `8 + length` in
// wrapping uint32 arithmetic before converting to int, so a Length field
// above 0xFFFFFFF7 wrapped to a small value instead of the huge one it
// actually represents. It is not exploitable today only because the
// len(b) != wantTotal check happens to reject these frames either way (no
// caller in this repo ever presents Decode with a real multi-GB buffer);
// this test pins that rejection so a future caller-path change can't quietly
// resurrect the wraparound as a real bug.
func TestDecodeLengthNearUint32Wraparound(t *testing.T) {
	//fusa:test REQ-CODEC-002
	// length = 0xFFFFFFF8: 8 + length wraps to 0 in uint32 arithmetic, but
	// 8 + int64(length) correctly evaluates to ~4.29 billion. Either way the
	// frame (16 bytes) cannot match wantTotal, but a wrapped computation
	// could — for other length values near this boundary — coincidentally
	// equal a small len(b) and be wrongly accepted; the widened computation
	// must not permit that.
	b := make([]byte, codec.HeaderSize)
	binary.BigEndian.PutUint32(b[4:8], 0xFFFFFFF8)
	_, err := codec.Decode(b)
	if !errors.Is(err, codec.ErrLengthMismatch) {
		t.Errorf("expected ErrLengthMismatch, got %v", err)
	}
}

func TestEncodeAppendsToDst(t *testing.T) {
	//fusa:test REQ-CODEC-001
	prefix := []byte("HEADER:")
	dst := make([]byte, len(prefix), len(prefix)+64)
	copy(dst, prefix)

	msg := someip.Message{ServiceID: 0x0001, MethodID: 0x0001}
	out := codec.Encode(dst, msg)

	if !bytes.HasPrefix(out, prefix) {
		t.Errorf("Encode did not preserve prefix: %q", out[:len(prefix)])
	}
	if len(out) != len(prefix)+codec.HeaderSize {
		t.Errorf("output length: got %d, want %d", len(out), len(prefix)+codec.HeaderSize)
	}
}

func TestEncodeProtocolVersionDefault(t *testing.T) {
	//fusa:test REQ-CODEC-001
	msg := someip.Message{ServiceID: 0x0001, MethodID: 0x0001}
	frame := codec.Encode(nil, msg)
	got, err := codec.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProtocolVersion != 0x01 {
		t.Errorf("default ProtocolVersion: got 0x%02x, want 0x01", got.ProtocolVersion)
	}
}

func TestDecodeRejectsWrongProtocolVersion(t *testing.T) {
	//fusa:test REQ-CODEC-002
	//fusa:test REQ-PROTO-001
	frame := make([]byte, codec.HeaderSize)
	// Set Length field to minimum (8).
	frame[4], frame[5], frame[6], frame[7] = 0x00, 0x00, 0x00, 0x08
	frame[12] = 0x02 // wrong protocol version
	_, err := codec.Decode(frame)
	if !errors.Is(err, someip.ErrWrongProtocolVersion) {
		t.Errorf("wrong ProtocolVersion: want ErrWrongProtocolVersion, got %v", err)
	}
}
