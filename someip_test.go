// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package someip_test

import (
	"errors"
	"testing"

	relay "github.com/SoundMatt/RELAY"
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
	eventID := someip.EventID(methodID)
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
		{"MsgTypeRequest", someip.MsgTypeRequest, 0x00},
		{"MsgTypeRequestNoReturn", someip.MsgTypeRequestNoReturn, 0x01},
		{"MsgTypeNotification", someip.MsgTypeNotification, 0x02},
		{"MsgTypeResponse", someip.MsgTypeResponse, 0x80},
		{"MsgTypeError", someip.MsgTypeError, 0x81},
		{"MsgTypeTPRequest", someip.MsgTypeTPRequest, 0x20},
		{"MsgTypeTPRequestNoReturn", someip.MsgTypeTPRequestNoReturn, 0x21},
		{"MsgTypeTPNotification", someip.MsgTypeTPNotification, 0x22},
		{"MsgTypeTPResponse", someip.MsgTypeTPResponse, 0xa0},
		{"MsgTypeTPError", someip.MsgTypeTPError, 0xa1},
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
		{"RetOK", someip.RetOK, 0x00},
		{"RetNotOK", someip.RetNotOK, 0x01},
		{"RetUnknownService", someip.RetUnknownService, 0x02},
		{"RetUnknownMethod", someip.RetUnknownMethod, 0x03},
		{"RetNotReady", someip.RetNotReady, 0x04},
		{"RetNotReachable", someip.RetNotReachable, 0x05},
		{"RetTimeout", someip.RetTimeout, 0x06},
		{"RetWrongProtocolVersion", someip.RetWrongProtocolVersion, 0x07},
		{"RetWrongInterfaceVersion", someip.RetWrongInterfaceVersion, 0x08},
		{"RetMalformedMessage", someip.RetMalformedMessage, 0x09},
		{"RetWrongMessageType", someip.RetWrongMessageType, 0x0a},
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
	if !errors.Is(someip.ErrClosed, relay.ErrClosed) {
		t.Error("ErrClosed must wrap relay.ErrClosed")
	}
}

func TestErrTimeout(t *testing.T) {
	//fusa:test REQ-ERR-002
	if someip.ErrTimeout == nil {
		t.Error("ErrTimeout must not be nil")
	}
	if !errors.Is(someip.ErrTimeout, relay.ErrTimeout) {
		t.Error("ErrTimeout must wrap relay.ErrTimeout")
	}
}

func TestErrNotConnected(t *testing.T) {
	//fusa:test REQ-ERR-003
	if someip.ErrNotConnected == nil {
		t.Error("ErrNotConnected must not be nil")
	}
	if !errors.Is(someip.ErrNotConnected, relay.ErrNotConnected) {
		t.Error("ErrNotConnected must wrap relay.ErrNotConnected")
	}
}

func TestErrPayloadTooLarge(t *testing.T) {
	//fusa:test REQ-ERR-007
	if someip.ErrPayloadTooLarge == nil {
		t.Error("ErrPayloadTooLarge must not be nil")
	}
	if !errors.Is(someip.ErrPayloadTooLarge, relay.ErrPayloadTooLarge) {
		t.Error("ErrPayloadTooLarge must wrap relay.ErrPayloadTooLarge")
	}
}

func TestErrUnknownMethod(t *testing.T) {
	//fusa:test REQ-ERR-004
	if someip.ErrUnknownMethod == nil {
		t.Error("ErrUnknownMethod must not be nil")
	}
	if !errors.Is(someip.ErrUnknownMethod, relay.ErrNotConnected) {
		t.Error("ErrUnknownMethod must wrap relay.ErrNotConnected")
	}
}

func TestErrUnknownService(t *testing.T) {
	//fusa:test REQ-ERR-005
	if someip.ErrUnknownService == nil {
		t.Error("ErrUnknownService must not be nil")
	}
	if !errors.Is(someip.ErrUnknownService, relay.ErrNotConnected) {
		t.Error("ErrUnknownService must wrap relay.ErrNotConnected")
	}
}

func TestErrMalformedMessage(t *testing.T) {
	//fusa:test REQ-ERR-006
	if someip.ErrMalformedMessage == nil {
		t.Error("ErrMalformedMessage must not be nil")
	}
	if !errors.Is(someip.ErrMalformedMessage, relay.ErrPayloadTooLarge) {
		t.Error("ErrMalformedMessage must wrap relay.ErrPayloadTooLarge")
	}
}

func TestErrWrongProtocolVersion(t *testing.T) {
	//fusa:test REQ-ERR-008
	if someip.ErrWrongProtocolVersion == nil {
		t.Error("ErrWrongProtocolVersion must not be nil")
	}
	// A message with a bad protocol version fails Validate with this sentinel,
	// and the sentinel is distinct from ErrMalformedMessage.
	err := someip.Message{ProtocolVersion: 0x02}.Validate()
	if !errors.Is(err, someip.ErrWrongProtocolVersion) {
		t.Errorf("Validate(protoVer=2): want ErrWrongProtocolVersion, got %v", err)
	}
	if errors.Is(err, someip.ErrMalformedMessage) {
		t.Error("ErrWrongProtocolVersion must be distinct from ErrMalformedMessage")
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	//fusa:test REQ-ERR-001
	//fusa:test REQ-ERR-002
	//fusa:test REQ-ERR-003
	//fusa:test REQ-ERR-004
	//fusa:test REQ-ERR-005
	//fusa:test REQ-ERR-006
	//fusa:test REQ-ERR-007
	errs := []error{
		someip.ErrClosed,
		someip.ErrTimeout,
		someip.ErrNotConnected,
		someip.ErrPayloadTooLarge,
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

// ── SOMEIPProtocolVersion + SpecVersion ──────────────────────────────────────

func TestSOMEIPProtocolVersion(t *testing.T) {
	//fusa:test REQ-PROTO-001
	if someip.SOMEIPProtocolVersion != 0x01 {
		t.Errorf("SOMEIPProtocolVersion: got 0x%02x, want 0x01", someip.SOMEIPProtocolVersion)
	}
}

func TestSpecVersion(t *testing.T) {
	//fusa:test REQ-SPEC-001
	if someip.SpecVersion != "1.0" {
		t.Errorf("SpecVersion: got %q, want %q", someip.SpecVersion, "1.0")
	}
	// Drift detector: our declared spec version must match the pinned RELAY
	// module's spec version. If a dependency bump changes relay.SpecVersion,
	// this fails until SpecVersion is deliberately re-reviewed and updated.
	if someip.SpecVersion != relay.SpecVersion {
		t.Errorf("SpecVersion %q != relay.SpecVersion %q — review RELAY spec changes before bumping",
			someip.SpecVersion, relay.SpecVersion)
	}
}

// ── SubscriberConfig ──────────────────────────────────────────────────────────

func TestSubscriberConfigDefaults(t *testing.T) {
	//fusa:test REQ-SUB-001
	cfg := someip.ApplySubscriberOpts(nil)
	if got := cfg.ChanDepth(64); got != 64 {
		t.Errorf("default ChanDepth: got %d, want 64", got)
	}
}

func TestSubscriberConfigWithChannelDepth(t *testing.T) {
	//fusa:test REQ-SUB-001
	cfg := someip.ApplySubscriberOpts([]someip.SubscriberOption{someip.WithChannelDepth(128)})
	if got := cfg.ChanDepth(64); got != 128 {
		t.Errorf("WithChannelDepth: got %d, want 128", got)
	}
}

func TestWithBackPressure(t *testing.T) {
	//fusa:test REQ-SUB-003
	//fusa:test REQ-SUB-004
	cfg := someip.ApplySubscriberOpts([]someip.SubscriberOption{someip.WithBackPressure(someip.DropOldest)})
	if cfg.BackPressure != someip.DropOldest {
		t.Errorf("WithBackPressure: got %v, want DropOldest", cfg.BackPressure)
	}
}

// ── ToMessage / FromMessage ───────────────────────────────────────────────────

func TestToMessage(t *testing.T) {
	//fusa:test REQ-ADAPT-002
	m := someip.Message{
		ServiceID:        0x1234,
		MethodID:         0x0001,
		ClientID:         5,
		SessionID:        10,
		InterfaceVersion: 2,
		MessageType:      someip.MsgTypeRequest,
		ReturnCode:       someip.RetOK,
		Payload:          []byte{0xDE, 0xAD},
	}
	rm := m.ToMessage()
	if rm.ID != "4660/1" {
		t.Errorf("ToMessage ID: got %q, want %q", rm.ID, "4660/1")
	}
	if string(rm.Payload) != string(m.Payload) {
		t.Error("ToMessage Payload mismatch")
	}
	// v0.3: someip.msg_type is the NUMERIC type; msg_type_name is the label.
	want := map[string]string{
		"someip.client_id":         "5",
		"someip.session_id":        "10",
		"someip.msg_type":          "0",
		"someip.msg_type_name":     "request",
		"someip.return_code":       "0",
		"someip.interface_version": "2",
	}
	for k, v := range want {
		if rm.Meta[k] != v {
			t.Errorf("ToMessage Meta[%q]: got %q, want %q", k, rm.Meta[k], v)
		}
	}
	if len(rm.Meta) != len(want) {
		t.Errorf("ToMessage Meta key count: got %d, want %d (%v)", len(rm.Meta), len(want), rm.Meta)
	}
}

func TestFromMessage(t *testing.T) {
	//fusa:test REQ-ADAPT-003
	rm := relay.Message{
		ID:      "4660/1",
		Payload: []byte{0xBE, 0xEF},
		Meta: map[string]string{
			"someip.client_id":         "5",
			"someip.session_id":        "10",
			"someip.msg_type":          "128", // MsgTypeResponse (0x80)
			"someip.msg_type_name":     "ignored-diagnostic-label",
			"someip.return_code":       "1",
			"someip.interface_version": "2",
		},
	}
	m, err := someip.FromMessage(rm)
	if err != nil {
		t.Fatalf("FromMessage: unexpected error: %v", err)
	}
	if m.ServiceID != 0x1234 || m.MethodID != 0x0001 {
		t.Errorf("FromMessage IDs: got 0x%04x/0x%04x, want 0x1234/0x0001", m.ServiceID, m.MethodID)
	}
	if m.ClientID != 5 || m.SessionID != 10 {
		t.Errorf("FromMessage client/session: got %d/%d, want 5/10", m.ClientID, m.SessionID)
	}
	if m.MessageType != someip.MsgTypeResponse {
		t.Errorf("FromMessage MessageType: got 0x%02x, want 0x80", m.MessageType)
	}
	if m.ReturnCode != someip.RetNotOK {
		t.Errorf("FromMessage ReturnCode: got 0x%02x, want 0x01", m.ReturnCode)
	}
	if m.InterfaceVersion != 2 {
		t.Errorf("FromMessage InterfaceVersion: got %d, want 2", m.InterfaceVersion)
	}
	if m.ProtocolVersion != someip.SOMEIPProtocolVersion {
		t.Errorf("FromMessage ProtocolVersion: got %d, want %d", m.ProtocolVersion, someip.SOMEIPProtocolVersion)
	}
}

// TestMessageRoundTripLossless verifies the v0.3 losslessness guarantee
// (hazard H-002): every header field survives Message → relay.Message → Message.
func TestMessageRoundTripLossless(t *testing.T) {
	//fusa:test REQ-ADAPT-002
	//fusa:test REQ-ADAPT-003
	orig := someip.Message{
		ServiceID:        0xABCD,
		MethodID:         0x8001,
		ClientID:         0x0042,
		SessionID:        0x00FF,
		ProtocolVersion:  someip.SOMEIPProtocolVersion,
		InterfaceVersion: 3,
		MessageType:      someip.MsgTypeNotification,
		ReturnCode:       someip.RetNotReachable,
		Payload:          []byte("telemetry"),
	}
	got, err := someip.FromMessage(orig.ToMessage())
	if err != nil {
		t.Fatalf("round-trip FromMessage: %v", err)
	}
	if got.ServiceID != orig.ServiceID || got.MethodID != orig.MethodID ||
		got.ClientID != orig.ClientID || got.SessionID != orig.SessionID ||
		got.ProtocolVersion != orig.ProtocolVersion || got.InterfaceVersion != orig.InterfaceVersion ||
		got.MessageType != orig.MessageType || got.ReturnCode != orig.ReturnCode ||
		string(got.Payload) != string(orig.Payload) {
		t.Errorf("round-trip not lossless:\n orig=%+v\n got =%+v", orig, got)
	}
}

func TestMessageTypeString(t *testing.T) {
	//fusa:test REQ-MSG-001
	cases := map[someip.MessageType]string{
		someip.MsgTypeRequest:         "request",
		someip.MsgTypeRequestNoReturn: "request_no_return",
		someip.MsgTypeNotification:    "notification",
		someip.MsgTypeResponse:        "response",
		someip.MsgTypeError:           "error",
		someip.MsgTypeTPRequest:       "tp_request",
		someip.MessageType(0x7F):      "127", // unknown → decimal
	}
	for mt, want := range cases {
		if got := mt.String(); got != want {
			t.Errorf("MessageType(0x%02x).String(): got %q, want %q", uint8(mt), got, want)
		}
	}
}

func TestFromMessageMalformed(t *testing.T) {
	//fusa:test REQ-ADAPT-003
	cases := []string{"", "notanid", "abc/xyz", "65536/1", "1/65536"}
	for _, id := range cases {
		_, err := someip.FromMessage(relay.Message{ID: id})
		if !errors.Is(err, someip.ErrMalformedMessage) {
			t.Errorf("FromMessage(%q): want ErrMalformedMessage, got %v", id, err)
		}
	}
}
