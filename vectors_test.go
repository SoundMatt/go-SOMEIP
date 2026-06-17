// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package someip_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	relay "github.com/SoundMatt/RELAY"
	someip "github.com/SoundMatt/go-SOMEIP"
)

// vector is the on-disk golden reference vector format mirrored from the RELAY
// spec (spec/vectors/*.json). These fixtures pin go-SOMEIP's ToMessage() /
// FromMessage() / Validate() behaviour to the canonical RELAY v0.3 contract.
type vector struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Kind        string          `json:"kind"`
	Value       json.RawMessage `json:"value"`
	Message     json.RawMessage `json:"message"`
	Error       string          `json:"error"`
}

// TestGoldenVectors verifies every committed golden vector under
// testdata/vectors/. Message vectors must (1) produce exactly the stored
// relay.Message from ToMessage (timestamp excluded) and (2) round-trip back to
// the canonical value via FromMessage. Error vectors must be rejected by
// Message.Validate with the named sentinel.
//
//fusa:test REQ-ADAPT-002
//fusa:test REQ-ADAPT-003
//fusa:test REQ-PROTO-002
func TestGoldenVectors(t *testing.T) {
	paths, err := filepath.Glob("testdata/vectors/*.json")
	if err != nil {
		t.Fatalf("glob vectors: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no golden vectors found under testdata/vectors/")
	}

	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			checkVector(t, p)
		})
	}
}

func checkVector(t *testing.T, path string) {
	t.Helper()

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read %s: %v", path, rerr)
	}
	var v vector
	if uerr := json.Unmarshal(data, &v); uerr != nil {
		t.Fatalf("unmarshal vector: %v", uerr)
	}
	if v.Type != "someip.Message" {
		t.Fatalf("unexpected vector type %q", v.Type)
	}

	var value someip.Message
	if uerr := json.Unmarshal(v.Value, &value); uerr != nil {
		t.Fatalf("unmarshal value: %v", uerr)
	}

	if v.Kind == "error" {
		verr := value.Validate()
		switch v.Error {
		case "ErrWrongProtocolVersion":
			if !errors.Is(verr, someip.ErrWrongProtocolVersion) {
				t.Errorf("Validate: want ErrWrongProtocolVersion, got %v", verr)
			}
		default:
			t.Fatalf("unhandled error vector %q", v.Error)
		}
		return
	}

	// Message vector: ToMessage must match the stored envelope (timestamp is
	// stamped with time.Now and excluded from comparison).
	var want relay.Message
	if uerr := json.Unmarshal(v.Message, &want); uerr != nil {
		t.Fatalf("unmarshal expected message: %v", uerr)
	}
	got := value.ToMessage()
	got.Timestamp = want.Timestamp
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", "  ")
		wj, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("ToMessage mismatch\n got: %s\nwant: %s", gj, wj)
	}

	// Round-trip: FromMessage(envelope) must reproduce the canonical value.
	back, ferr := someip.FromMessage(want)
	if ferr != nil {
		t.Fatalf("FromMessage: %v", ferr)
	}
	if !reflect.DeepEqual(back, value) {
		t.Errorf("round-trip mismatch\n got: %+v\nwant: %+v", back, value)
	}
}
