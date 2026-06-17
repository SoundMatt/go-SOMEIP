// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package someip defines the Go interface for SOME/IP communication.
//
// SOME/IP (Scalable service-Oriented MiddlewarE over IP) is the AUTOSAR
// on-wire protocol for service-oriented communication over automotive Ethernet.
//
// The API is intentionally narrow: it covers the primitives needed for
// automotive service communication — method calls, fire-and-forget messages,
// and event subscriptions.
//
// Choose an implementation by importing one of the sub-packages:
//
//	import "github.com/SoundMatt/go-SOMEIP/mock" // in-process, no network
//	import "github.com/SoundMatt/go-SOMEIP/udp"  // SOME/IP over UDP
//	import "github.com/SoundMatt/go-SOMEIP/tcp"  // SOME/IP over TCP
//
// All three expose constructors that satisfy the [Service] and [Server]
// interfaces defined in this package.
//
// # RELAY conformance
//
// This package conforms to RELAY spec v0.2 (SpecVersion). Use [Adapt] to
// obtain a [relay.Caller] from any [Service]. Use [Message.ToMessage] and
// [FromMessage] to convert between native and envelope representations.
package someip

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	relay "github.com/SoundMatt/RELAY"
)

// SpecVersion is the RELAY specification version this package conforms to (spec §17.12).
//
//fusa:req REQ-SPEC-001
const SpecVersion = "0.2"

// SOMEIPProtocolVersion is the protocol version byte placed in every SOME/IP header.
// Inbound messages with a different value MUST be rejected with [ErrMalformedMessage].
//
//fusa:req REQ-PROTO-001
const SOMEIPProtocolVersion uint8 = 0x01

// ── Sentinel errors ───────────────────────────────────────────────────────────
//
// The four mandatory RELAY sentinels are wrapped so that errors.Is(err, relay.ErrXxx)
// returns true for any error returned by this package (RELAY spec §5.2).

//fusa:req REQ-ERR-001
var ErrClosed = fmt.Errorf("someip: closed: %w", relay.ErrClosed)

//fusa:req REQ-ERR-002
var ErrTimeout = fmt.Errorf("someip: timeout: %w", relay.ErrTimeout)

//fusa:req REQ-ERR-003
// ErrNotConnected is returned when an operation is attempted before a connection
// is established, or after the underlying transport has dropped.
var ErrNotConnected = fmt.Errorf("someip: not connected: %w", relay.ErrNotConnected)

//fusa:req REQ-ERR-007
var ErrPayloadTooLarge = fmt.Errorf("someip: payload too large: %w", relay.ErrPayloadTooLarge)

// Protocol-specific errors (RELAY spec §5.4). Each wraps the closest mandatory sentinel.

//fusa:req REQ-ERR-004
var ErrUnknownMethod = fmt.Errorf("someip: unknown method: %w", relay.ErrNotConnected)

//fusa:req REQ-ERR-005
var ErrUnknownService = fmt.Errorf("someip: unknown service: %w", relay.ErrNotConnected)

//fusa:req REQ-ERR-006
var ErrMalformedMessage = fmt.Errorf("someip: malformed message: %w", relay.ErrPayloadTooLarge)

// ── Wire-type definitions ─────────────────────────────────────────────────────

//fusa:req REQ-TYPES-001
// ServiceID is the 16-bit SOME/IP service identifier.
type ServiceID uint16

//fusa:req REQ-TYPES-002
// MethodID is the 16-bit SOME/IP method/event identifier.
type MethodID uint16

//fusa:req REQ-TYPES-003
// ClientID is the 16-bit SOME/IP client identifier.
type ClientID uint16

//fusa:req REQ-TYPES-004
// SessionID is the 16-bit SOME/IP session identifier, auto-incremented per call.
type SessionID uint16

//fusa:req REQ-TYPES-006
// EventID is a MethodID that identifies a SOME/IP event (notification).
// By AUTOSAR convention event IDs have bit 15 set (0x8000–0xFFFF).
type EventID = MethodID

//fusa:req REQ-TYPES-005
// InstanceID is the 16-bit SOME/IP instance identifier.
type InstanceID uint16

// ── MessageType ───────────────────────────────────────────────────────────────

//fusa:req REQ-MSG-001

// MessageType identifies the SOME/IP message type field.
type MessageType uint8

const (
	// MsgTypeRequest is a method call expecting a response (0x00).
	MsgTypeRequest MessageType = 0x00
	// MsgTypeRequestNoReturn is a fire-and-forget method call (0x01).
	MsgTypeRequestNoReturn MessageType = 0x01
	// MsgTypeNotification is an event or field notification (0x02).
	MsgTypeNotification MessageType = 0x02
	// MsgTypeResponse is a successful reply to a MsgTypeRequest (0x80).
	MsgTypeResponse MessageType = 0x80
	// MsgTypeError is an error reply to a MsgTypeRequest (0x81).
	MsgTypeError MessageType = 0x81

	// TP variants carry SOME/IP-TP segmented payloads.
	MsgTypeTPRequest         MessageType = 0x20
	MsgTypeTPRequestNoReturn MessageType = 0x21
	MsgTypeTPNotification    MessageType = 0x22
	MsgTypeTPResponse        MessageType = 0xa0
	MsgTypeTPError           MessageType = 0xa1
)

// ── ReturnCode ────────────────────────────────────────────────────────────────

//fusa:req REQ-MSG-002

// ReturnCode is the SOME/IP return code field.
type ReturnCode uint8

const (
	// RetOK indicates successful processing (E_OK).
	RetOK ReturnCode = 0x00
	// RetNotOK indicates a generic error (E_NOT_OK).
	RetNotOK ReturnCode = 0x01
	// RetUnknownService indicates the requested service is unknown (E_UNKNOWN_SERVICE).
	RetUnknownService ReturnCode = 0x02
	// RetUnknownMethod indicates the requested method is unknown (E_UNKNOWN_METHOD).
	RetUnknownMethod ReturnCode = 0x03
	// RetNotReady indicates the service is not yet initialised (E_NOT_READY).
	RetNotReady ReturnCode = 0x04
	// RetNotReachable indicates the service cannot be contacted (E_NOT_REACHABLE).
	RetNotReachable ReturnCode = 0x05
	// RetTimeout indicates a timeout occurred on the server side (E_TIMEOUT).
	RetTimeout ReturnCode = 0x06
	// RetWrongProtocolVersion indicates a protocol version mismatch (E_WRONG_PROTOCOL_VERSION).
	RetWrongProtocolVersion ReturnCode = 0x07
	// RetWrongInterfaceVersion indicates an interface version mismatch (E_WRONG_INTERFACE_VERSION).
	RetWrongInterfaceVersion ReturnCode = 0x08
	// RetMalformedMessage indicates a deserialisation error (E_MALFORMED_MESSAGE).
	RetMalformedMessage ReturnCode = 0x09
	// RetWrongMessageType indicates an unexpected message type (E_WRONG_MESSAGE_TYPE).
	RetWrongMessageType ReturnCode = 0x0a
)

// ── Message ───────────────────────────────────────────────────────────────────

//fusa:req REQ-MSG-003

// Message is a decoded SOME/IP message.
// The Payload field contains only the application payload (no header bytes).
type Message struct {
	// ServiceID identifies the target service.
	ServiceID ServiceID `json:"service_id"`
	// MethodID identifies the method or event within the service.
	MethodID MethodID `json:"method_id"`
	// ClientID identifies the originating client. Zero for server-initiated messages.
	ClientID ClientID `json:"client_id"`
	// SessionID is the per-client request counter used for request/response correlation.
	SessionID SessionID `json:"session_id"`
	// ProtocolVersion is the SOME/IP protocol version; MUST be SOMEIPProtocolVersion (0x01).
	ProtocolVersion uint8 `json:"protocol_version"`
	// InterfaceVersion is the major version of the service interface.
	InterfaceVersion uint8 `json:"interface_version"`
	// MessageType classifies the message (MsgTypeRequest, MsgTypeResponse, etc.).
	MessageType MessageType `json:"message_type"`
	// ReturnCode carries the processing result (RetOK, RetNotOK, etc.).
	ReturnCode ReturnCode `json:"return_code"`
	// Payload is the raw application payload bytes.
	Payload []byte `json:"payload,omitempty"`
}

// ToMessage converts m to the universal relay.Message envelope (RELAY spec §15.7.6).
//
//fusa:req REQ-ADAPT-002
func (m Message) ToMessage() relay.Message {
	return relay.Message{
		Protocol:  relay.SOMEIP,
		ID:        fmt.Sprintf("%d/%d", uint16(m.ServiceID), uint16(m.MethodID)),
		Payload:   m.Payload,
		Timestamp: time.Now(),
		Meta: map[string]string{
			"someip.msg_type":          msgTypeName(m.MessageType),
			"someip.return_code":       strconv.Itoa(int(m.ReturnCode)),
			"someip.interface_version": strconv.Itoa(int(m.InterfaceVersion)),
		},
	}
}

// FromMessage converts a relay.Message envelope back to a native Message
// (RELAY spec §15.7.6). The ID field MUST be "serviceID/methodID" in decimal.
// Returns ErrMalformedMessage if the ID is malformed.
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
	return Message{
		ServiceID:       ServiceID(svcID),
		MethodID:        MethodID(methodID),
		Payload:         msg.Payload,
		ProtocolVersion: SOMEIPProtocolVersion,
	}, nil
}

func msgTypeName(mt MessageType) string {
	switch mt {
	case MsgTypeRequest:
		return "request"
	case MsgTypeRequestNoReturn:
		return "request_no_return"
	case MsgTypeNotification:
		return "notification"
	case MsgTypeResponse:
		return "response"
	case MsgTypeError:
		return "error"
	default:
		return strconv.Itoa(int(mt))
	}
}

// ── Handler types ─────────────────────────────────────────────────────────────

//fusa:req REQ-SERVER-001
// MethodHandler is called by a [Server] to process an incoming method request.
// Returning a non-nil error causes the server to send an Error response (REQ-SERVER-003).
type MethodHandler func(ctx context.Context, req Message) ([]byte, error)

// ── Subscriber helpers (RELAY spec §14.1) ─────────────────────────────────────
//
// These re-export the canonical RELAY subscriber types so callers can use either
// someip.SubscriberConfig or relay.SubscriberConfig interchangeably.

//fusa:req REQ-SUB-001

// SubscriberConfig holds per-subscription options.
type SubscriberConfig = relay.SubscriberConfig

// SubscriberOption configures a subscription at creation time.
type SubscriberOption = relay.SubscriberOption

//fusa:req REQ-SUB-003
// BackPressurePolicy controls what happens when a subscription channel is full.
type BackPressurePolicy = relay.BackPressurePolicy

const (
	DropNewest = relay.DropNewest // drop the arriving sample (default)
	DropOldest = relay.DropOldest // drop the oldest buffered sample
	Block      = relay.Block      // block until space is available
)

// WithChannelDepth sets the capacity of the event delivery channel.
//
//fusa:req REQ-SUB-001
func WithChannelDepth(n int) SubscriberOption { return relay.WithChannelDepth(n) }

// WithBackPressure sets the back-pressure policy applied when the channel is full.
//
//fusa:req REQ-SUB-004
func WithBackPressure(p BackPressurePolicy) SubscriberOption { return relay.WithBackPressure(p) }

// ApplySubscriberOpts merges a slice of SubscriberOption into a SubscriberConfig.
//
//fusa:req REQ-SUB-001
func ApplySubscriberOpts(opts []SubscriberOption) SubscriberConfig {
	return relay.ApplySubscriberOpts(opts)
}

// ── Interfaces ────────────────────────────────────────────────────────────────

//fusa:req REQ-SERVICE-001
//fusa:req REQ-SERVICE-002
//fusa:req REQ-SERVICE-003

// Service is a client-side handle to a SOME/IP service instance.
// A Service is safe for concurrent use from multiple goroutines.
type Service interface {
	// Call invokes method methodID and waits for the response.
	// Returns [ErrTimeout] if no response arrives within the configured deadline,
	// [ErrClosed] if the service is closed, or [ErrUnknownService] if the
	// target is not reachable.
	Call(ctx context.Context, methodID MethodID, payload []byte) (Message, error)

	// CallNoReturn sends a fire-and-forget request (MsgTypeRequestNoReturn).
	// No response is expected; the method returns as soon as the frame is sent.
	CallNoReturn(ctx context.Context, methodID MethodID, payload []byte) error

	// Subscribe creates a [Subscription] for event eventID.
	// Returns [ErrClosed] if the service is closed.
	Subscribe(eventID EventID, opts ...SubscriberOption) (Subscription, error)

	// Close releases all resources held by the service.
	Close() error
}

//fusa:req REQ-SERVER-002
//fusa:req REQ-SERVER-003

// Server hosts a SOME/IP service instance.
// A Server is safe for concurrent use from multiple goroutines.
type Server interface {
	// RegisterMethod registers handler for methodID.
	// Replaces any previously registered handler for the same ID.
	RegisterMethod(methodID MethodID, handler MethodHandler) error

	// Emit publishes event eventID to all current subscribers.
	// Returns [ErrClosed] if the server is closed.
	Emit(eventID EventID, payload []byte) error

	// Close stops the server and releases all resources.
	Close() error
}

//fusa:req REQ-SUB-001
//fusa:req REQ-SUB-002

// Subscription delivers SOME/IP event notifications for a single event ID.
// A Subscription is safe for concurrent use from multiple goroutines.
type Subscription interface {
	// C returns the channel on which event messages are delivered.
	// The channel is closed when the subscription or owning Service is closed.
	C() <-chan Message

	// Unsubscribe removes this subscription; no further events are delivered.
	// The channel returned by C is NOT closed by Unsubscribe.
	Unsubscribe() error

	// Close unsubscribes and closes the message channel.
	Close() error
}

// ── RELAY application interface (spec §10.3) ─────────────────────────────────

//fusa:req REQ-ADAPT-001

// Adapt wraps s as a [relay.Caller], enabling protocol-agnostic application code
// (RELAY spec §10.3). Use [Message.ToMessage] / [FromMessage] for message conversion.
//
// Note: relay.Node.Subscribe on the returned adapter returns [ErrNotConnected];
// SOME/IP event subscriptions require an EventID — use [Service.Subscribe] directly.
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
	if msg.Meta["someip.msg_type"] == "request_no_return" {
		return a.s.CallNoReturn(ctx, m.MethodID, m.Payload)
	}
	_, err = a.s.Call(ctx, m.MethodID, m.Payload)
	return err
}

// Subscribe returns ErrNotConnected — SOMEIP event subscriptions require an EventID.
// Use [Service.Subscribe] to subscribe to a specific event by EventID.
func (a *serviceAdapter) Subscribe(_ ...relay.SubscriberOption) (<-chan relay.Message, error) {
	return nil, ErrNotConnected
}

func (a *serviceAdapter) Close() error { return a.s.Close() }

// ── Optional interfaces (RELAY spec §9) ───────────────────────────────────────
//
// Implementations that satisfy these interfaces MUST use these exact signatures.
// Declare satisfied interfaces in the capabilities output (cmd/go-someip).

// HealthStatus represents the operational health of a node.
type HealthStatus int

const (
	HealthOK       HealthStatus = 0
	HealthDegraded HealthStatus = 1
	HealthDown     HealthStatus = 2
)

// Health carries health status and optional diagnostic detail.
type Health struct {
	Status  HealthStatus `json:"status"`
	Details string       `json:"details,omitempty"`
}

//fusa:req REQ-OPT-001
// HealthProvider is the optional health interface (RELAY spec §9).
type HealthProvider interface {
	Health() Health
}

// Metrics carries runtime counters for observability.
type Metrics struct {
	WriteCount     uint64 `json:"write_count"`
	DeliverCount   uint64 `json:"deliver_count"`
	DropCount      uint64 `json:"drop_count"`
	BytesWritten   uint64 `json:"bytes_written"`
	BytesDelivered uint64 `json:"bytes_delivered"`
	ErrorCount     uint64 `json:"error_count"`
}

//fusa:req REQ-OPT-002
// MetricsProvider is the optional metrics interface (RELAY spec §9).
type MetricsProvider interface {
	Metrics() Metrics
}

//fusa:req REQ-OPT-003
// Drainer is the optional graceful-shutdown interface (RELAY spec §9).
type Drainer interface {
	CloseWithDrain(ctx context.Context) error
}
