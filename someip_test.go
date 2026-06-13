// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package someip_test

import (
	"errors"
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
)

// ── Named types ───────────────────────────────────────────────────────────────

func TestServiceIDType(t *testing.T) {
	//fusa:test REQ-TYPES-001
	var id someip.ServiceID = 0xFFFF
	if uint16(id) != 0xFFFF {
		t.Errorf("ServiceID underlying uint16 roundtrip failed: got 0x%04x", uint16(id))
	}
}

func TestMethodIDType(t *testing.T) {
	//fusa:test REQ-TYPES-002
	var id someip.MethodID = 0x8001
	if uint16(id) != 0x8001 {
		t.Errorf("MethodID underlying uint16 roundtrip failed: got 0x%04x", uint16(id))
	}
}

func TestClientIDType(t *testing.T) {
	//fusa:test REQ-TYPES-003
	var id someip.ClientID = 0x0001
	if uint16(id) != 0x0001 {
		t.Errorf("ClientID underlying uint16 roundtrip failed: got 0x%04x", uint16(id))
	}
}

func TestSessionIDType(t *testing.T) {
	//fusa:test REQ-TYPES-004
	var id someip.SessionID = 0xABCD
	if uint16(id) != 0xABCD {
		t.Errorf("SessionID underlying uint16 roundtrip failed: got 0x%04x", uint16(id))
	}
}

func TestInstanceIDType(t *testing.T) {
	//fusa:test REQ-TYPES-005
	var id someip.InstanceID = 0x0001
	if uint16(id) != 0x0001 {
		t.Errorf("InstanceID underlying uint16 roundtrip failed: got 0x%04x", uint16(id))
	}
}

func TestEventIDIsMethodID(t *testing.T) {
	//fusa:test REQ-TYPES-006
	// EventID is a type alias for MethodID; they must be interchangeable.
	var methodID someip.MethodID = 0x8001
	var eventID someip.EventID = methodID
	if eventID != methodID {
		t.Errorf("EventID != MethodID: got 0x%04x, want 0x%04x", eventID, methodID)
	}
}

// ── MessageType constants ─────────────────────────────────────────────────────

func TestMessageTypeConstants(t *testing.T) {
	//fusa:test REQ-MSG-001
	cases := []struct {
		name string
		got  someip.MessageType
		want someip.MessageType
	}{
		{"Request", someip.Request, 0x00},
		{"RequestNoReturn", someip.RequestNoReturn, 0x01},
		{"Notification", someip.Notification, 0x02},
		{"Response", someip.Response, 0x80},
		{"Error", someip.Error, 0x81},
		{"TPRequest", someip.TPRequest, 0x20},
		{"TPRequestNoReturn", someip.TPRequestNoReturn, 0x21},
		{"TPNotification", someip.TPNotification, 0x22},
		{"TPResponse", someip.TPResponse, 0xa0},
		{"TPError", someip.TPError, 0xa1},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got 0x%02x, want 0x%02x", tc.name, tc.got, tc.want)
		}
	}
}

// ── ReturnCode constants ──────────────────────────────────────────────────────

func TestReturnCodeConstants(t *testing.T) {
	//fusa:test REQ-MSG-002
	cases := []struct {
		name string
		got  someip.ReturnCode
		want someip.ReturnCode
	}{
		{"OK", someip.OK, 0x00},
		{"NotOK", someip.NotOK, 0x01},
		{"UnknownService", someip.UnknownService, 0x02},
		{"UnknownMethod", someip.UnknownMethod, 0x03},
		{"NotReady", someip.NotReady, 0x04},
		{"NotReachable", someip.NotReachable, 0x05},
		{"Timeout", someip.Timeout, 0x06},
		{"WrongProtocolVersion", someip.WrongProtocolVersion, 0x07},
		{"WrongInterfaceVersion", someip.WrongInterfaceVersion, 0x08},
		{"MalformedMessage", someip.MalformedMessage, 0x09},
		{"WrongMessageType", someip.WrongMessageType, 0x0a},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got 0x%02x, want 0x%02x", tc.name, tc.got, tc.want)
		}
	}
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

func TestErrClosed(t *testing.T) {
	//fusa:test REQ-ERR-001
	if someip.ErrClosed == nil {
		t.Error("ErrClosed must not be nil")
	}
	if !errors.Is(someip.ErrClosed, someip.ErrClosed) {
		t.Error("ErrClosed must be comparable via errors.Is")
	}
}

func TestErrTimeout(t *testing.T) {
	//fusa:test REQ-ERR-002
	if someip.ErrTimeout == nil {
		t.Error("ErrTimeout must not be nil")
	}
	if !errors.Is(someip.ErrTimeout, someip.ErrTimeout) {
		t.Error("ErrTimeout must be comparable via errors.Is")
	}
}

func TestErrNotReady(t *testing.T) {
	//fusa:test REQ-ERR-003
	if someip.ErrNotReady == nil {
		t.Error("ErrNotReady must not be nil")
	}
	if !errors.Is(someip.ErrNotReady, someip.ErrNotReady) {
		t.Error("ErrNotReady must be comparable via errors.Is")
	}
}

func TestErrUnknownMethod(t *testing.T) {
	//fusa:test REQ-ERR-004
	if someip.ErrUnknownMethod == nil {
		t.Error("ErrUnknownMethod must not be nil")
	}
	if !errors.Is(someip.ErrUnknownMethod, someip.ErrUnknownMethod) {
		t.Error("ErrUnknownMethod must be comparable via errors.Is")
	}
}

func TestErrUnknownService(t *testing.T) {
	//fusa:test REQ-ERR-005
	if someip.ErrUnknownService == nil {
		t.Error("ErrUnknownService must not be nil")
	}
	if !errors.Is(someip.ErrUnknownService, someip.ErrUnknownService) {
		t.Error("ErrUnknownService must be comparable via errors.Is")
	}
}

func TestErrMalformedMessage(t *testing.T) {
	//fusa:test REQ-ERR-006
	if someip.ErrMalformedMessage == nil {
		t.Error("ErrMalformedMessage must not be nil")
	}
	if !errors.Is(someip.ErrMalformedMessage, someip.ErrMalformedMessage) {
		t.Error("ErrMalformedMessage must be comparable via errors.Is")
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	//fusa:test REQ-ERR-001
	//fusa:test REQ-ERR-002
	//fusa:test REQ-ERR-003
	//fusa:test REQ-ERR-004
	//fusa:test REQ-ERR-005
	//fusa:test REQ-ERR-006
	errs := []error{
		someip.ErrClosed,
		someip.ErrTimeout,
		someip.ErrNotReady,
		someip.ErrUnknownMethod,
		someip.ErrUnknownService,
		someip.ErrMalformedMessage,
	}
	for i, a := range errs {
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors[%d] and [%d] must not be equal", i, j)
			}
		}
	}
}

// ── SubscribeConfig ───────────────────────────────────────────────────────────

func TestSubscribeConfigDefaults(t *testing.T) {
	//fusa:test REQ-SUB-001
	cfg := someip.ApplySubscribeOpts(nil)
	if got := cfg.ChanDepth(64); got != 64 {
		t.Errorf("default ChanDepth: got %d, want 64", got)
	}
}

func TestSubscribeConfigWithChannelDepth(t *testing.T) {
	//fusa:test REQ-SUB-001
	cfg := someip.ApplySubscribeOpts([]someip.SubscribeOption{someip.WithChannelDepth(128)})
	if got := cfg.ChanDepth(64); got != 128 {
		t.Errorf("WithChannelDepth: got %d, want 128", got)
	}
}
