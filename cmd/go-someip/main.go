// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command go-someip is the RELAY-conformant CLI for the go-SOMEIP library
// (RELAY spec §11, §12). It exposes version, capabilities, and status commands
// whose JSON output validates against the §12 schemas (verified by
// `relay conform`).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"

	relay "github.com/SoundMatt/RELAY"
	someip "github.com/SoundMatt/go-SOMEIP"
)

// toolName is the binary name reported in every CLI document.
const toolName = "go-someip"

// binVersion is the semantic version of this binary. Overridable via
// -ldflags "-X main.binVersion=X.Y.Z" at build time.
var binVersion = "1.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "version":
		runVersion(os.Args[2:])
	case "capabilities":
		runCapabilities()
	case "status":
		runStatus(os.Args[2:])
	case "convert":
		os.Exit(runConvert(os.Args[2:]))
	case "send":
		os.Exit(runSend(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "go-someip: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: go-someip <command> [flags]")
	fmt.Fprintln(os.Stderr, "Commands: version, capabilities, status, convert, send")
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// versionDoc is the version document per RELAY spec §12.1 (cli-version.json).
type versionDoc struct {
	Tool        string `json:"tool"`
	Protocol    string `json:"protocol"`
	ProtocolInt int    `json:"protocol_int"`
	Version     string `json:"version"`
	SpecVersion string `json:"spec_version"`
	Language    string `json:"language"`
	Runtime     string `json:"runtime"`
}

func runVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text or json")
	_ = fs.Parse(args)

	if *format == "json" {
		writeJSON(versionDoc{
			Tool:        toolName,
			Protocol:    relay.SOMEIP.String(),
			ProtocolInt: int(relay.SOMEIP),
			Version:     binVersion,
			SpecVersion: someip.SpecVersion,
			Language:    "go",
			Runtime:     runtime.Version(),
		})
		return
	}
	fmt.Printf("%s  protocol=%s  version=%s  spec=%s  runtime=%s\n",
		toolName, relay.SOMEIP, binVersion, someip.SpecVersion, runtime.Version())
}

// capabilitiesDoc is the capabilities document per RELAY spec §12.2
// (cli-capabilities.json).
type capabilitiesDoc struct {
	Kind               string   `json:"kind"`
	Tool               string   `json:"tool"`
	Protocol           string   `json:"protocol"`
	ProtocolInt        int      `json:"protocol_int"`
	Version            string   `json:"version"`
	SpecVersion        string   `json:"spec_version"`
	Commands           []string `json:"commands"`
	Transports         []string `json:"transports"`
	Features           []string `json:"features"`
	Interfaces         []string `json:"interfaces"`
	OptionalInterfaces []string `json:"optional_interfaces"`
	Adapt              bool     `json:"adapt"`
}

func runCapabilities() {
	writeJSON(capabilitiesDoc{
		Kind:               "capabilities",
		Tool:               toolName,
		Protocol:           relay.SOMEIP.String(),
		ProtocolInt:        int(relay.SOMEIP),
		Version:            binVersion,
		SpecVersion:        someip.SpecVersion,
		Commands:           []string{"version", "capabilities", "status", "convert", "send"},
		Transports:         []string{"mock", "udp", "tcp"},
		Features:           []string{"sd", "tp", "e2e"},
		Interfaces:         []string{"Node", "Caller"},
		OptionalInterfaces: []string{},
		Adapt:              true,
	})
}

// statusDoc is the status document per RELAY spec §12.3 (cli-status.json).
type statusDoc struct {
	Tool      string         `json:"tool"`
	Protocol  string         `json:"protocol"`
	Version   string         `json:"version"`
	Healthy   bool           `json:"healthy"`
	Connected bool           `json:"connected"`
	Endpoint  string         `json:"endpoint"`
	Details   map[string]any `json:"details"`
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text or json")
	_ = fs.Parse(args)

	if *format == "json" {
		writeJSON(statusDoc{
			Tool:      toolName,
			Protocol:  relay.SOMEIP.String(),
			Version:   binVersion,
			Healthy:   true,
			Connected: false,
			Endpoint:  "",
			Details:   map[string]any{"spec_version": someip.SpecVersion},
		})
		return
	}
	fmt.Printf("%s  protocol=%s  version=%s  healthy=true  connected=false\n",
		toolName, relay.SOMEIP, binVersion)
}
