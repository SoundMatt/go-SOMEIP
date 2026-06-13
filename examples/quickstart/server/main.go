// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command server hosts a SOME/IP quickstart service over UDP.
// Part of the Docker quickstart.
//
// Methods:
//
//	0x0001 Echo  — returns the request payload unchanged
//	0x0002 Version — returns "go-SOMEIP v0.1"
//
// Events:
//
//	0x8001 Heartbeat — emitted every second
//
// Environment variables:
//
//	SOMEIP_ADDR  Listen address (default: 0.0.0.0:30509)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/udp"
)

func main() {
	addr := envOrDefault("SOMEIP_ADDR", "0.0.0.0:30509")

	srv, err := udp.NewServer(udp.ServerConfig{
		Addr:             addr,
		ServiceID:        0x1234,
		InstanceID:       0x0001,
		InterfaceVersion: 0x01,
	})
	if err != nil {
		log.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	log.Printf("go-SOMEIP server listening on %s (ServiceID=0x1234, InstanceID=0x0001)", addr)

	_ = srv.RegisterMethod(0x0001, func(_ context.Context, req someip.Message) ([]byte, error) {
		log.Printf("echo method called, payload=%q", req.Payload)
		return req.Payload, nil
	})

	_ = srv.RegisterMethod(0x0002, func(_ context.Context, _ someip.Message) ([]byte, error) {
		return []byte("go-SOMEIP v0.1"), nil
	})

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for tick := range ticker.C {
		hb := fmt.Sprintf("heartbeat ts=%d", tick.Unix())
		if err := srv.Emit(0x8001, []byte(hb)); err != nil {
			log.Printf("Emit: %v", err)
		}
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
