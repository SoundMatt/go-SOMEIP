// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package mock_test

import (
	"context"
	"testing"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/mock"
)

// FuzzCall feeds arbitrary method IDs and payloads to the mock transport.
// The transport must not panic regardless of input.
func FuzzCall(f *testing.F) {
	f.Add(uint16(0x0001), []byte("hello"))
	f.Add(uint16(0x0000), []byte(nil))
	f.Add(uint16(0xFFFF), []byte("large payload that exceeds normal bounds"))

	f.Fuzz(func(t *testing.T, methodID uint16, payload []byte) {
		bus := mock.NewBus()
		srv, _ := bus.NewServer(someip.ServiceID(0x1234), someip.InstanceID(0x0001))
		defer srv.Close()

		_ = srv.RegisterMethod(someip.MethodID(methodID), func(_ context.Context, req someip.Message) ([]byte, error) {
			return req.Payload, nil
		})

		svc, _ := bus.NewService(someip.ServiceID(0x1234), someip.InstanceID(0x0001))
		defer svc.Close()

		_, _ = svc.Call(context.Background(), someip.MethodID(methodID), payload)
	})
}
