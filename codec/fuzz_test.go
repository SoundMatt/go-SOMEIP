// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package codec_test

import (
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
)

// FuzzDecode feeds arbitrary byte slices to Decode.
// It must not panic regardless of input.
func FuzzDecode(f *testing.F) {
	f.Add(knownGoodFrame)
	f.Add([]byte{})
	f.Add(make([]byte, codec.HeaderSize))

	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = codec.Decode(b)
	})
}

// FuzzEncodeRoundTrip encodes a message from fuzz-derived fields and
// verifies that Decode produces an equivalent message.
func FuzzEncodeRoundTrip(f *testing.F) {
	f.Add(
		uint16(0x1234), uint16(0x0001),
		uint16(0x0005), uint16(0x0001),
		uint8(0x01), uint8(0x01),
		uint8(0x00), uint8(0x00),
		[]byte("ping"),
	)

	f.Fuzz(func(
		t *testing.T,
		svcID, methID uint16,
		clientID, sessionID uint16,
		protoVer, ifaceVer uint8,
		msgType, retCode uint8,
		payload []byte,
	) {
		msg := someip.Message{
			ServiceID:        someip.ServiceID(svcID),
			MethodID:         someip.MethodID(methID),
			ClientID:         someip.ClientID(clientID),
			SessionID:        someip.SessionID(sessionID),
			ProtocolVersion:  protoVer,
			InterfaceVersion: ifaceVer,
			MessageType:      someip.MessageType(msgType),
			ReturnCode:       someip.ReturnCode(retCode),
			Payload:          payload,
		}

		frame := codec.Encode(nil, msg)
		got, err := codec.Decode(frame)
		if err != nil {
			t.Fatalf("Decode of Encode output failed: %v", err)
		}
		if got.ServiceID != msg.ServiceID {
			t.Errorf("ServiceID mismatch: got 0x%04x want 0x%04x", got.ServiceID, msg.ServiceID)
		}
		if got.MethodID != msg.MethodID {
			t.Errorf("MethodID mismatch")
		}
		if len(got.Payload) != len(msg.Payload) {
			t.Errorf("Payload length mismatch: got %d want %d", len(got.Payload), len(msg.Payload))
		}
	})
}
