// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// The RELAY adapter: Adapt, Message.ToMessage, and FromMessage. This file is
// the `adapt` module of the RELAY spec §13.7.1 cross-language library
// architecture — the entry point that turns a native [Service] into a
// protocol-agnostic [relay.Caller], and the lossless conversions between a
// native [Message] and the universal [relay.Message] envelope (spec §15.7.6).

package someip

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	relay "github.com/SoundMatt/RELAY"
)

// ToMessage converts m to the universal relay.Message envelope (RELAY spec §15.7.6).
//
// The conversion is lossless (spec v0.3, hazard H-002): every SOME/IP header
// field is carried either in the ID ("serviceID/methodID") or in Meta.
// "someip.msg_type" carries the numeric MessageType for exact round-trip
// fidelity; "someip.msg_type_name" carries the human-readable label for
// diagnostics only and is ignored by [FromMessage].
//
//fusa:req REQ-ADAPT-002
func (m Message) ToMessage() relay.Message {
	return relay.Message{
		Protocol:  relay.SOMEIP,
		ID:        fmt.Sprintf("%d/%d", uint16(m.ServiceID), uint16(m.MethodID)),
		Payload:   m.Payload,
		Timestamp: time.Now(),
		Meta: map[string]string{
			"someip.client_id":         strconv.FormatUint(uint64(m.ClientID), 10),
			"someip.session_id":        strconv.FormatUint(uint64(m.SessionID), 10),
			"someip.msg_type":          strconv.FormatUint(uint64(m.MessageType), 10),
			"someip.msg_type_name":     m.MessageType.String(),
			"someip.return_code":       strconv.FormatUint(uint64(m.ReturnCode), 10),
			"someip.interface_version": strconv.FormatUint(uint64(m.InterfaceVersion), 10),
		},
	}
}

// FromMessage converts a relay.Message envelope back to a native Message
// (RELAY spec §15.7.6). The ID field MUST be "serviceID/methodID" in decimal.
// Returns ErrMalformedMessage if the ID is malformed.
//
// The conversion is lossless (spec v0.3): ClientID, SessionID, MessageType,
// ReturnCode, and InterfaceVersion are restored from Meta. Missing Meta keys
// default to zero; "someip.msg_type_name" is diagnostic and ignored.
//
// ProtocolVersion is version-normalising: it is always set to
// [SOMEIPProtocolVersion] (0x01, the only valid value) regardless of the
// envelope, since there is no someip.protocol_version Meta key. Wrong-version
// detection is therefore enforced only at the wire boundary by codec.Decode,
// not through the adapter/Node API.
//
//fusa:req REQ-ADAPT-003
func FromMessage(msg relay.Message) (Message, error) {
	parts := strings.SplitN(msg.ID, "/", 2)
	if len(parts) != 2 {
		return Message{}, fmt.Errorf("%w: invalid SOMEIP ID %q", ErrMalformedMessage, msg.ID)
	}
	svcID, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return Message{}, fmt.Errorf("%w: invalid service ID in %q", ErrMalformedMessage, msg.ID)
	}
	methodID, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return Message{}, fmt.Errorf("%w: invalid method ID in %q", ErrMalformedMessage, msg.ID)
	}
	m := Message{
		ServiceID:       ServiceID(svcID),
		MethodID:        MethodID(methodID),
		Payload:         msg.Payload,
		ProtocolVersion: SOMEIPProtocolVersion,
	}
	if v := metaUint(msg.Meta, "someip.client_id", 16); v != nil {
		m.ClientID = ClientID(*v)
	}
	if v := metaUint(msg.Meta, "someip.session_id", 16); v != nil {
		m.SessionID = SessionID(*v)
	}
	if v := metaUint(msg.Meta, "someip.msg_type", 8); v != nil {
		m.MessageType = MessageType(*v)
	}
	if v := metaUint(msg.Meta, "someip.return_code", 8); v != nil {
		m.ReturnCode = ReturnCode(*v)
	}
	if v := metaUint(msg.Meta, "someip.interface_version", 8); v != nil {
		m.InterfaceVersion = uint8(*v)
	}
	return m, nil
}

// metaUint parses Meta[key] as an unsigned integer of the given bit size.
// Returns nil when the key is absent or empty so callers leave the field at
// its zero value; a malformed value is treated as absent.
func metaUint(meta map[string]string, key string, bits int) *uint64 {
	s, ok := meta[key]
	if !ok || s == "" {
		return nil
	}
	v, err := strconv.ParseUint(s, 10, bits)
	if err != nil {
		return nil
	}
	return &v
}

// ── RELAY application interface (spec §10.3) ─────────────────────────────────

//fusa:req REQ-ADAPT-001

// Adapt wraps s as a [relay.Caller], enabling protocol-agnostic application code
// (RELAY spec §10.3). Use [Message.ToMessage] / [FromMessage] for message conversion.
//
// The returned adapter's Subscribe reads the [relay.WithEventID] option to
// determine which SOME/IP event group to subscribe to (RELAY spec §14.1);
// it returns [ErrNotConnected] if no EventID is supplied.
func Adapt(s Service) relay.Caller {
	return &serviceAdapter{s: s}
}

type serviceAdapter struct{ s Service }

func (a *serviceAdapter) Protocol() relay.Protocol { return relay.SOMEIP }

func (a *serviceAdapter) Call(ctx context.Context, req relay.Message) (relay.Message, error) {
	m, err := FromMessage(req)
	if err != nil {
		return relay.Message{}, err
	}
	resp, err := a.s.Call(ctx, m.MethodID, m.Payload)
	if err != nil {
		return relay.Message{}, err
	}
	return resp.ToMessage(), nil
}

func (a *serviceAdapter) Send(ctx context.Context, msg relay.Message) error {
	m, err := FromMessage(msg)
	if err != nil {
		return err
	}
	if m.MessageType == MsgTypeRequestNoReturn {
		return a.s.CallNoReturn(ctx, m.MethodID, m.Payload)
	}
	_, err = a.s.Call(ctx, m.MethodID, m.Payload)
	return err
}

// Subscribe subscribes to the SOME/IP event group named by [relay.WithEventID]
// (RELAY spec §14.1) and returns a channel of converted relay.Messages.
// Returns [ErrNotConnected] if no EventID was supplied. Channel-depth and
// back-pressure options are forwarded to the underlying [Service.Subscribe].
func (a *serviceAdapter) Subscribe(opts ...relay.SubscriberOption) (<-chan relay.Message, error) {
	cfg := relay.ApplySubscriberOpts(opts)
	if cfg.EventID == 0 {
		return nil, fmt.Errorf("%w: Subscribe requires relay.WithEventID for SOME/IP", ErrNotConnected)
	}
	sub, err := a.s.Subscribe(EventID(cfg.EventID), opts...)
	if err != nil {
		return nil, err
	}
	out := make(chan relay.Message, cfg.ChanDepth(64))
	go func() {
		defer close(out)
		for m := range sub.C() {
			rm := m.ToMessage()
			// Honour the RELAY BackPressurePolicy (spec §10.5(3)) rather than
			// blocking unconditionally: a stalled subscriber must not stall the
			// underlying Service delivery under the DropNewest/DropOldest policies.
			switch cfg.BackPressure {
			case relay.Block:
				out <- rm
			case relay.DropOldest:
				// Drain the oldest buffered sample to make room, then enqueue.
				for {
					select {
					case out <- rm:
					default:
						select {
						case <-out:
						default:
						}
						continue
					}
					break
				}
			default: // relay.DropNewest (spec default): drop the arriving sample when full.
				select {
				case out <- rm:
				default:
				}
			}
		}
	}()
	return out, nil
}

func (a *serviceAdapter) Close() error { return a.s.Close() }
