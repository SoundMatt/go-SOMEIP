// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	relay "github.com/SoundMatt/RELAY/v2"
	someip "github.com/SoundMatt/go-SOMEIP"
	"github.com/SoundMatt/go-SOMEIP/codec"
)

// runSend implements the RELAY spec §11.2 streaming JSON sink:
// `send --format json` with no protocol flags reads a stream of relay.Message
// values as NDJSON on stdin (one per line) and publishes each until EOF. This
// is the egress dual of `subscribe --format json` and the portable sink used
// by `relay crossbar`.
//
// The protocol-flag form of send (`--service`/`--method`/`--payload`, §11.2's
// per-protocol flag table) is a separate, not-yet-implemented conformance gap
// tracked outside this issue; only the streaming JSON sink is implemented
// here.
//
// Exit codes: 0 sent (possibly with some per-line errors logged to stderr),
// 1 if any message failed to send, 2 invalid args.
func runSend(args []string) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "text", "output format: text or json")
	transport := fs.String("transport", "udp", "destination transport: udp or tcp")
	endpoint := fs.String("endpoint", "", "destination address (host:port)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: send: %v\n", err)
		return 2
	}
	if *format != "json" {
		fmt.Fprintln(os.Stderr, "go-someip: send: only 'send --format json' (the streaming NDJSON sink, RELAY spec §11.2) is implemented; the per-protocol --service/--method flag form is not yet supported")
		return 2
	}
	if *endpoint == "" {
		fmt.Fprintln(os.Stderr, "go-someip: send: --endpoint is required")
		return 2
	}
	network, ok := dialNetwork(*transport)
	if !ok {
		fmt.Fprintf(os.Stderr, "go-someip: send: unsupported transport %q (want udp or tcp)\n", *transport)
		return 2
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(context.Background(), network, *endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: send: dial %s %s: %v\n", network, *endpoint, err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	sent, failed := 0, 0
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rm relay.Message
		if err := json.Unmarshal([]byte(line), &rm); err != nil {
			fmt.Fprintf(os.Stderr, "go-someip: send: invalid relay.Message JSON: %v\n", err)
			failed++
			continue
		}
		m, err := someip.FromMessage(rm)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-someip: send: %v\n", err)
			failed++
			continue
		}
		frame := codec.Encode(nil, m)
		if _, err := conn.Write(frame); err != nil {
			fmt.Fprintf(os.Stderr, "go-someip: send: write: %v\n", err)
			failed++
			continue
		}
		sent++
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: send: read stdin: %v\n", err)
		return 1
	}

	writeJSON(map[string]any{"sent": sent, "failed": failed})
	if failed > 0 {
		return 1
	}
	return 0
}

// dialNetwork maps a --transport flag value to a Go net.Dial network name.
func dialNetwork(transport string) (string, bool) {
	switch strings.ToLower(transport) {
	case "udp":
		return "udp4", true
	case "tcp":
		return "tcp4", true
	default:
		return "", false
	}
}
