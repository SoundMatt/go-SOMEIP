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
package someip

import (
	"context"
	"errors"
)

// ── Sentinel errors ───────────────────────────────────────────────────────────

//fusa:req REQ-ERR-001
// ErrClosed is returned when an operation is called on a closed entity.
var ErrClosed = errors.New("someip: entity is closed")

//fusa:req REQ-ERR-002
// ErrTimeout is returned when a request does not receive a response in time.
var ErrTimeout = errors.New("someip: request timed out")

//fusa:req REQ-ERR-003
// ErrNotReady is returned when the service is not yet available.
var ErrNotReady = errors.New("someip: service not ready")

//fusa:req REQ-ERR-004
// ErrUnknownMethod is returned when a method ID is not registered on the server.
var ErrUnknownMethod = errors.New("someip: unknown method")

//fusa:req REQ-ERR-005
// ErrUnknownService is returned when the requested service/instance is not found.
var ErrUnknownService = errors.New("someip: unknown service")

//fusa:req REQ-ERR-006
// ErrMalformedMessage is returned when a received frame cannot be parsed.
var ErrMalformedMessage = errors.New("someip: malformed message")

// ── Wire-type aliases ─────────────────────────────────────────────────────────

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

// ── Message type ─────────────────────────────────────────────────────────────

//fusa:req REQ-MSG-001

// MessageType identifies the SOME/IP message type field.
type MessageType uint8

const (
	// Request is a method call expecting a response (0x00).
	Request MessageType = 0x00
	// RequestNoReturn is a fire-and-forget method call (0x01).
	RequestNoReturn MessageType = 0x01
	// Notification is an event or field notification (0x02).
	Notification MessageType = 0x02
	// Response is a successful reply to a Request (0x80).
	Response MessageType = 0x80
	// Error is an error reply to a Request (0x81).
	Error MessageType = 0x81

	// TP variants carry SOME/IP-TP segmented payloads.
	TPRequest          MessageType = 0x20
	TPRequestNoReturn  MessageType = 0x21
	TPNotification     MessageType = 0x22
	TPResponse         MessageType = 0xa0
	TPError            MessageType = 0xa1
)

// ── Return code ───────────────────────────────────────────────────────────────

//fusa:req REQ-MSG-002

// ReturnCode is the SOME/IP return code field.
type ReturnCode uint8

const (
	// OK indicates successful processing (E_OK).
	OK ReturnCode = 0x00
	// NotOK indicates a generic error (E_NOT_OK).
	NotOK ReturnCode = 0x01
	// UnknownService indicates the requested service is unknown (E_UNKNOWN_SERVICE).
	UnknownService ReturnCode = 0x02
	// UnknownMethod indicates the requested method is unknown (E_UNKNOWN_METHOD).
	UnknownMethod ReturnCode = 0x03
	// NotReady indicates the service is not yet initialised (E_NOT_READY).
	NotReady ReturnCode = 0x04
	// NotReachable indicates the service cannot be contacted (E_NOT_REACHABLE).
	NotReachable ReturnCode = 0x05
	// Timeout indicates a timeout occurred on the server side (E_TIMEOUT).
	Timeout ReturnCode = 0x06
	// WrongProtocolVersion indicates a protocol version mismatch (E_WRONG_PROTOCOL_VERSION).
	WrongProtocolVersion ReturnCode = 0x07
	// WrongInterfaceVersion indicates an interface version mismatch (E_WRONG_INTERFACE_VERSION).
	WrongInterfaceVersion ReturnCode = 0x08
	// MalformedMessage indicates a deserialisation error (E_MALFORMED_MESSAGE).
	MalformedMessage ReturnCode = 0x09
	// WrongMessageType indicates an unexpected message type (E_WRONG_MESSAGE_TYPE).
	WrongMessageType ReturnCode = 0x0a
)

// ── Message ───────────────────────────────────────────────────────────────────

//fusa:req REQ-MSG-003

// Message is a decoded SOME/IP message.
// The Payload field contains only the application payload (no header bytes).
type Message struct {
	// ServiceID identifies the target service.
	ServiceID ServiceID
	// MethodID identifies the method or event within the service.
	MethodID MethodID
	// ClientID identifies the originating client. Zero for server-initiated messages.
	ClientID ClientID
	// SessionID is the per-client request counter used for request/response correlation.
	SessionID SessionID
	// ProtocolVersion is the SOME/IP protocol version; always 0x01 on the wire.
	ProtocolVersion uint8
	// InterfaceVersion is the major version of the service interface.
	InterfaceVersion uint8
	// MessageType classifies the message (Request, Response, Notification, etc.).
	MessageType MessageType
	// ReturnCode carries the processing result (OK, NotOK, etc.).
	ReturnCode ReturnCode
	// Payload is the raw application payload bytes.
	Payload []byte
}

// ── Handler types ─────────────────────────────────────────────────────────────

//fusa:req REQ-SERVER-001
// MethodHandler is called by a [Server] to process an incoming method request.
// Returning a non-nil error causes the server to send an Error response (REQ-SERVER-003).
type MethodHandler func(ctx context.Context, req Message) ([]byte, error)

// ── Subscribe options ─────────────────────────────────────────────────────────

//fusa:req REQ-SUB-001

// SubscribeConfig holds per-subscription options.
type SubscribeConfig struct {
	// ChannelDepth is the capacity of the subscription's delivery channel.
	// 0 means the implementation default (64).
	ChannelDepth int
}

// SubscribeOption configures a subscription at creation time.
type SubscribeOption func(*SubscribeConfig)

// WithChannelDepth sets the capacity of the event delivery channel.
func WithChannelDepth(n int) SubscribeOption {
	return func(c *SubscribeConfig) { c.ChannelDepth = n }
}

// ApplySubscribeOpts merges a slice of SubscribeOption into a SubscribeConfig.
func ApplySubscribeOpts(opts []SubscribeOption) SubscribeConfig {
	var c SubscribeConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// ChanDepth returns the resolved channel depth.
func (c SubscribeConfig) ChanDepth(defaultDepth int) int {
	if c.ChannelDepth > 0 {
		return c.ChannelDepth
	}
	return defaultDepth
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

	// CallNoReturn sends a fire-and-forget request (RequestNoReturn).
	// No response is expected; the method returns as soon as the frame is sent.
	CallNoReturn(ctx context.Context, methodID MethodID, payload []byte) error

	// Subscribe creates a [Subscription] for event eventID.
	// Returns [ErrClosed] if the service is closed.
	Subscribe(eventID EventID, opts ...SubscribeOption) (Subscription, error)

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
