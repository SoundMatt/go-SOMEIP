// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	someip "github.com/SoundMatt/go-SOMEIP"
)

// runConvert implements `convert --protocol SOMEIP [--format json]`, the
// RELAY spec §11.2 black-box driver used by `relay interop`. It reads one
// canonical someip.Message value as JSON on stdin, runs it through this
// implementation's own [someip.Message.Validate] and [someip.Message.ToMessage]
// — the same code path used at runtime — and writes the resulting
// relay.Message as JSON on stdout, with Timestamp zeroed so results are
// comparable across implementations.
//
// Exit codes: 0 converted, 1 invalid input, 2 invalid args.
func runConvert(args []string) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	protocol := fs.String("protocol", "", "protocol of the canonical value (must be SOMEIP)")
	format := fs.String("format", "json", "output format: json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: convert: %v\n", err)
		return 2
	}
	if *protocol == "" {
		fmt.Fprintln(os.Stderr, "go-someip: convert: --protocol is required")
		return 2
	}
	if !strings.EqualFold(*protocol, "SOMEIP") {
		fmt.Fprintf(os.Stderr, "go-someip: convert: unsupported protocol %q (this binary only converts SOMEIP)\n", *protocol)
		return 2
	}
	if *format != "json" {
		fmt.Fprintf(os.Stderr, "go-someip: convert: unsupported format %q\n", *format)
		return 2
	}

	value, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: convert: read stdin: %v\n", err)
		return 1
	}

	var m someip.Message
	if err := json.Unmarshal(value, &m); err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: convert: invalid canonical value: %v\n", err)
		return 1
	}
	if err := m.Validate(); err != nil {
		// Per spec §11.2: write the sentinel error name (§5) to stderr.
		fmt.Fprintf(os.Stderr, "go-someip: convert: %v\n", err)
		return 1
	}

	out := m.ToMessage()
	out.Timestamp = time.Time{} // normalise for cross-implementation comparison

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "    ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "go-someip: convert: %v\n", err)
		return 1
	}
	return 0
}
