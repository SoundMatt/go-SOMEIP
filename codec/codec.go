// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package codec implements SOME/IP wire-frame serialization and deserialization.
//
// The SOME/IP header is 16 bytes:
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|          Service ID           |           Method ID            |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                            Length                             |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|          Client ID            |           Session ID           |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|Proto Ver|Interface Ver| Msg Type  |  Return Code  |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// Length counts the bytes from Client ID to end of payload (i.e. 8 + len(payload)).
// The Service Discovery magic cookie uses ServiceID=0xFFFF, MethodID=0x8100.
package codec

import (
	"encoding/binary"
	"errors"
	"fmt"

	someip "github.com/SoundMatt/go-SOMEIP"
)

// HeaderSize is the fixed size of a SOME/IP header in bytes.
const HeaderSize = 16

// minLength is the minimum value of the Length field (8 bytes: ClientID through ReturnCode).
const minLength = 8

// ErrShortFrame is returned when a byte slice is too short to contain a SOME/IP header.
var ErrShortFrame = errors.New("codec: frame too short for SOME/IP header")

// ErrLengthMismatch is returned when the Length field does not match the actual frame size.
var ErrLengthMismatch = errors.New("codec: length field does not match frame size")

// ProtocolVersion is the SOME/IP protocol version placed in every header.
const ProtocolVersion uint8 = 0x01

// Encode serializes msg into a SOME/IP frame and appends it to dst.
// The returned slice shares backing memory with dst when capacity allows.
//
//fusa:req REQ-CODEC-001
func Encode(dst []byte, msg someip.Message) []byte {
	payloadLen := len(msg.Payload)
	frameLen := HeaderSize + payloadLen
	if cap(dst)-len(dst) < frameLen {
		grown := make([]byte, len(dst), len(dst)+frameLen)
		copy(grown, dst)
		dst = grown
	}
	start := len(dst)
	dst = dst[:start+frameLen]
	b := dst[start:]

	binary.BigEndian.PutUint16(b[0:2], uint16(msg.ServiceID))
	binary.BigEndian.PutUint16(b[2:4], uint16(msg.MethodID))
	// Length = 8 (ClientID…ReturnCode) + payload
	binary.BigEndian.PutUint32(b[4:8], uint32(minLength+payloadLen))
	binary.BigEndian.PutUint16(b[8:10], uint16(msg.ClientID))
	binary.BigEndian.PutUint16(b[10:12], uint16(msg.SessionID))

	proto := msg.ProtocolVersion
	if proto == 0 {
		proto = ProtocolVersion
	}
	b[12] = proto
	b[13] = msg.InterfaceVersion
	b[14] = uint8(msg.MessageType)
	b[15] = uint8(msg.ReturnCode)

	copy(b[16:], msg.Payload)
	return dst
}

// Decode parses a SOME/IP frame from b.
// It returns [ErrShortFrame] if b is shorter than [HeaderSize],
// or [ErrLengthMismatch] if the Length field is inconsistent with len(b).
//
//fusa:req REQ-CODEC-002
func Decode(b []byte) (someip.Message, error) {
	if len(b) < HeaderSize {
		return someip.Message{}, fmt.Errorf("%w: got %d bytes, need %d", ErrShortFrame, len(b), HeaderSize)
	}

	length := binary.BigEndian.Uint32(b[4:8])
	if length < minLength {
		return someip.Message{}, fmt.Errorf("%w: length field %d < minimum %d", ErrLengthMismatch, length, minLength)
	}
	wantTotal := int(8 + length) // 8 (ServiceID+MethodID+Length) + length
	if len(b) != wantTotal {
		return someip.Message{}, fmt.Errorf("%w: frame is %d bytes, length field implies %d", ErrLengthMismatch, len(b), wantTotal)
	}

	payloadLen := int(length) - minLength
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		copy(payload, b[HeaderSize:])
	}

	msg := someip.Message{
		ServiceID:        someip.ServiceID(binary.BigEndian.Uint16(b[0:2])),
		MethodID:         someip.MethodID(binary.BigEndian.Uint16(b[2:4])),
		ClientID:         someip.ClientID(binary.BigEndian.Uint16(b[8:10])),
		SessionID:        someip.SessionID(binary.BigEndian.Uint16(b[10:12])),
		ProtocolVersion:  b[12],
		InterfaceVersion: b[13],
		MessageType:      someip.MessageType(b[14]),
		ReturnCode:       someip.ReturnCode(b[15]),
		Payload:          payload,
	}
	return msg, nil
}
