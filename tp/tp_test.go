// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package tp_test

import (
	"testing"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/tp"
)

func makeMsg(payload []byte) someip.Message {
	return someip.Message{
		ServiceID:       0x1234,
		MethodID:        0x0001,
		SessionID:       0x0001,
		ProtocolVersion: 0x01,
		MessageType:     someip.MsgTypeRequest,
		ReturnCode:      someip.RetOK,
		Payload:         payload,
	}
}

// TestSegment_SmallPayload verifies that payloads within one segment are not TP-framed.
func TestSegment_SmallPayload(t *testing.T) {
	msg := makeMsg([]byte("hello"))
	segs, err := tp.Segment(msg, 0)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	if tp.IsTP(segs[0]) {
		t.Error("single segment should not have TP bit set")
	}
	if string(segs[0].Payload) != "hello" {
		t.Errorf("payload = %q, want %q", segs[0].Payload, "hello")
	}
}

// TestSegment_ExactFit verifies a payload exactly equal to segmentSize is one non-TP segment.
func TestSegment_ExactFit(t *testing.T) {
	payload := make([]byte, 32)
	segs, err := tp.Segment(makeMsg(payload), 32)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	if tp.IsTP(segs[0]) {
		t.Error("exact-fit payload should not be TP-framed")
	}
}

// TestSegment_MultiSegment verifies large payloads produce multiple TP segments.
func TestSegment_MultiSegment(t *testing.T) {
	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = byte(i)
	}
	segs, err := tp.Segment(makeMsg(payload), 32)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if len(segs) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segs))
	}
	for i, s := range segs {
		if !tp.IsTP(s) {
			t.Errorf("segment %d: TP bit not set", i)
		}
	}
	// Last segment must NOT have the More bit (bit 0 of TP header byte 3).
	last := segs[len(segs)-1]
	lastMore := last.Payload[3] & 0x01
	if lastMore != 0 {
		t.Error("last segment has More bit set")
	}
	// All but last must have More bit set.
	for i, s := range segs[:len(segs)-1] {
		if s.Payload[3]&0x01 == 0 {
			t.Errorf("segment %d: More bit not set", i)
		}
	}
}

// TestSegment_TooSmallSize verifies ErrSegmentTooLarge for size < 16.
func TestSegment_TooSmallSize(t *testing.T) {
	_, err := tp.Segment(makeMsg(make([]byte, 100)), 8)
	if err == nil {
		t.Error("expected ErrSegmentTooLarge, got nil")
	}
}

// TestReassembler_RoundTrip segments a message then reassembles it.
func TestReassembler_RoundTrip(t *testing.T) {
	sizes := []int{0, 1, 15, 16, 17, 32, 100, 1000, tp.DefaultSegmentSize - 1, tp.DefaultSegmentSize, tp.DefaultSegmentSize + 1, 8192}
	r := tp.NewReassembler(tp.ReassemblerConfig{})
	defer r.Close()

	for _, size := range sizes {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i % 251)
		}
		msg := makeMsg(payload)
		msg.SessionID = someip.SessionID(size % 0xFFFF)

		segs, err := tp.Segment(msg, 32)
		if err != nil {
			t.Fatalf("size %d: Segment: %v", size, err)
		}

		var got *someip.Message
		for _, seg := range segs {
			got, err = r.Add(seg)
			if err != nil {
				t.Fatalf("size %d: Add: %v", size, err)
			}
		}
		if got == nil {
			t.Fatalf("size %d: reassembly returned nil", size)
		}
		if len(got.Payload) != size {
			t.Errorf("size %d: payload len = %d", size, len(got.Payload))
			continue
		}
		for i, b := range got.Payload {
			if b != payload[i] {
				t.Errorf("size %d: payload[%d] = %d, want %d", size, i, b, payload[i])
				break
			}
		}
		if tp.IsTP(*got) {
			t.Errorf("size %d: reassembled message still has TP bit", size)
		}
	}
}

// TestReassembler_OutOfOrder verifies segments arriving out of order are reassembled correctly.
func TestReassembler_OutOfOrder(t *testing.T) {
	payload := make([]byte, 96)
	for i := range payload {
		payload[i] = byte(i)
	}
	msg := makeMsg(payload)
	segs, err := tp.Segment(msg, 32)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if len(segs) < 3 {
		t.Fatalf("expected >= 3 segments, got %d", len(segs))
	}

	r := tp.NewReassembler(tp.ReassemblerConfig{})
	defer r.Close()

	// Add in reverse order.
	reversed := make([]someip.Message, len(segs))
	copy(reversed, segs)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	var got *someip.Message
	for _, seg := range reversed {
		got, err = r.Add(seg)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if got == nil {
		t.Fatal("reassembly returned nil")
	}
	if string(got.Payload) != string(payload) {
		t.Error("out-of-order reassembly produced wrong payload")
	}
}

// TestReassembler_NonTPPassThrough verifies non-TP messages pass through immediately.
func TestReassembler_NonTPPassThrough(t *testing.T) {
	r := tp.NewReassembler(tp.ReassemblerConfig{})
	defer r.Close()

	msg := makeMsg([]byte("direct"))
	got, err := r.Add(msg)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got == nil {
		t.Fatal("non-TP message was not passed through")
	}
	if string(got.Payload) != "direct" {
		t.Errorf("payload = %q, want \"direct\"", got.Payload)
	}
}

// TestReassembler_Timeout verifies that expired assembly windows return ErrReassemblyTimeout.
func TestReassembler_Timeout(t *testing.T) {
	r := tp.NewReassembler(tp.ReassemblerConfig{
		Timeout:    50 * time.Millisecond,
		GCInterval: 10 * time.Millisecond,
	})
	defer r.Close()

	payload := make([]byte, 64)
	segs, err := tp.Segment(makeMsg(payload), 32)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}
	if len(segs) < 2 {
		t.Skip("need at least 2 segments to test timeout")
	}

	// Add only the first segment.
	_, err = r.Add(segs[0])
	if err != nil {
		t.Fatalf("Add first: %v", err)
	}

	// Wait for timeout.
	time.Sleep(100 * time.Millisecond)

	// Adding a subsequent segment to an expired window returns ErrReassemblyTimeout.
	_, err = r.Add(segs[1])
	if err == nil {
		t.Error("expected ErrReassemblyTimeout, got nil")
	}
}

// TestReassembler_MalformedSegment verifies malformed TP payloads return an error.
func TestReassembler_MalformedSegment(t *testing.T) {
	r := tp.NewReassembler(tp.ReassemblerConfig{})
	defer r.Close()

	bad := someip.Message{
		ServiceID:   0x1234,
		MethodID:    0x0001,
		SessionID:   0x0001,
		MessageType: someip.MsgTypeTPRequest,
		Payload:     []byte{0x00}, // too short for TP header
	}
	_, err := r.Add(bad)
	if err == nil {
		t.Error("expected ErrMalformedSegment, got nil")
	}
}

// TestReassembler_Close verifies Close discards pending windows without panic.
func TestReassembler_Close(t *testing.T) {
	r := tp.NewReassembler(tp.ReassemblerConfig{})

	payload := make([]byte, 64)
	segs, _ := tp.Segment(makeMsg(payload), 32)
	_, _ = r.Add(segs[0]) // first segment only — incomplete

	r.Close() // must not panic or deadlock
}

// TestBaseMessageType verifies stripping the TP bit.
func TestBaseMessageType(t *testing.T) {
	cases := []struct {
		in   someip.MessageType
		want someip.MessageType
	}{
		{someip.MsgTypeTPRequest, someip.MsgTypeRequest},
		{someip.MsgTypeTPRequestNoReturn, someip.MsgTypeRequestNoReturn},
		{someip.MsgTypeTPNotification, someip.MsgTypeNotification},
		{someip.MsgTypeTPResponse, someip.MsgTypeResponse},
		{someip.MsgTypeTPError, someip.MsgTypeError},
		{someip.MsgTypeRequest, someip.MsgTypeRequest}, // non-TP unchanged
	}
	for _, tc := range cases {
		got := tp.BaseMessageType(tc.in)
		if got != tc.want {
			t.Errorf("BaseMessageType(0x%02x) = 0x%02x, want 0x%02x", tc.in, got, tc.want)
		}
	}
}

// TestSegment_DuplicatesInReassembler verifies duplicate segments are handled safely.
func TestSegment_DuplicatesInReassembler(t *testing.T) {
	payload := make([]byte, 64)
	segs, err := tp.Segment(makeMsg(payload), 32)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}

	r := tp.NewReassembler(tp.ReassemblerConfig{})
	defer r.Close()

	// Send first segment twice.
	_, err = r.Add(segs[0])
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	_, err = r.Add(segs[0])
	if err != nil {
		t.Fatalf("duplicate Add: %v", err)
	}

	// Complete with remaining segments.
	var got *someip.Message
	for _, seg := range segs[1:] {
		got, err = r.Add(seg)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if got == nil {
		t.Fatal("reassembly returned nil with duplicate segment")
	}
	if len(got.Payload) != 64 {
		t.Errorf("payload len = %d, want 64", len(got.Payload))
	}
}
