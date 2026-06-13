// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command client connects to the go-SOMEIP quickstart server and demonstrates
// method calls and event subscription.
// Part of the Docker quickstart.
//
// Environment variables:
//
//	SOMEIP_SERVER  Server address (default: localhost:30509)
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/SoundMatt/go-SOMEIP/udp"
)

func main() {
	serverAddr := envOrDefault("SOMEIP_SERVER", "localhost:30509")

	svc, err := udp.NewService(udp.ServiceConfig{
		ServerAddr: serverAddr,
		ServiceID:  0x1234,
		InstanceID: 0x0001,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		log.Fatalf("NewService: %v", err)
	}
	defer func() { _ = svc.Close() }()

	log.Printf("connected to %s", serverAddr)

	// Method 0x0001: echo
	resp, err := svc.Call(context.Background(), 0x0001, []byte("hello from client"))
	if err != nil {
		log.Fatalf("Call echo: %v", err)
	}
	log.Printf("echo response: %q", resp.Payload)

	// Method 0x0002: version
	resp, err = svc.Call(context.Background(), 0x0002, nil)
	if err != nil {
		log.Fatalf("Call version: %v", err)
	}
	log.Printf("server version: %s", resp.Payload)

	// Event 0x8001: heartbeat subscription
	sub, err := svc.Subscribe(0x8001)
	if err != nil {
		log.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	log.Println("subscribed to heartbeat event (waiting 5 s)...")
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case msg, ok := <-sub.C():
			if !ok {
				return
			}
			log.Printf("heartbeat: %s", msg.Payload)
		case <-timeout.C:
			log.Println("done")
			return
		}
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
