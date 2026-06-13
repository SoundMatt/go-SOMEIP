// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package someip_test

import (
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
)

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
		{"TPResponse", someip.TPResponse, 0xa0},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got 0x%02x, want 0x%02x", tc.name, tc.got, tc.want)
		}
	}
}

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
		{"MalformedMessage", someip.MalformedMessage, 0x09},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got 0x%02x, want 0x%02x", tc.name, tc.got, tc.want)
		}
	}
}

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

func TestSentinelErrors(t *testing.T) {
	//fusa:test REQ-ERR-001
	errs := []error{
		someip.ErrClosed,
		someip.ErrTimeout,
		someip.ErrNotReady,
		someip.ErrUnknownMethod,
		someip.ErrUnknownService,
		someip.ErrMalformedMessage,
	}
	for _, err := range errs {
		if err == nil {
			t.Error("sentinel error must not be nil")
		}
	}
}
