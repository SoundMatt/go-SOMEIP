// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command go-someip is the RELAY-conformant CLI for the go-SOMEIP library
// (RELAY spec §11, §12). It exposes version, capabilities, and status commands
// as required by the RELAY conformance contract.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	someip "github.com/SoundMatt/go-SOMEIP"
)

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
	default:
		fmt.Fprintf(os.Stderr, "go-someip: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: go-someip <command> [flags]")
	fmt.Fprintln(os.Stderr, "Commands: version, capabilities, status")
}

func runVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text or json")
	_ = fs.Parse(args)

	if *format == "json" {
		out := map[string]string{
			"spec_version": someip.SpecVersion,
			"protocol":     "SOMEIP",
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
		return
	}
	fmt.Printf("go-someip  protocol=SOMEIP  spec=%s\n", someip.SpecVersion)
}

// capabilitiesDoc is the capabilities document per RELAY spec §12.2.
type capabilitiesDoc struct {
	Protocol           string   `json:"protocol"`
	SpecVersion        string   `json:"spec_version"`
	Adapt              bool     `json:"adapt"`
	Transports         []string `json:"transports"`
	OptionalInterfaces []string `json:"optional_interfaces"`
}

func runCapabilities() {
	doc := capabilitiesDoc{
		Protocol:           "SOMEIP",
		SpecVersion:        someip.SpecVersion,
		Adapt:              true,
		Transports:         []string{"mock", "udp", "tcp"},
		OptionalInterfaces: []string{},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

// statusDoc is the status document per RELAY spec §11.
type statusDoc struct {
	Protocol    string `json:"protocol"`
	SpecVersion string `json:"spec_version"`
	Status      string `json:"status"`
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text or json")
	_ = fs.Parse(args)

	if *format == "json" {
		doc := statusDoc{
			Protocol:    "SOMEIP",
			SpecVersion: someip.SpecVersion,
			Status:      "ok",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(doc)
		return
	}
	fmt.Printf("protocol=SOMEIP  spec=%s  status=ok\n", someip.SpecVersion)
}
