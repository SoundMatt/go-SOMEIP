// Copyright (c) 2026 Matt Jones. All rights reserved.
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package e2e_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SoundMatt/go-SOMEIP/e2e"
)

// ── Profile 01 ────────────────────────────────────────────────────────────────

func TestProfile01_RoundTrip(t *testing.T) {
	//fusa:test REQ-E2E-001
	//fusa:test REQ-E2E-002
	//fusa:test REQ-E2E-003
	//fusa:test REQ-E2E-004
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x1234})
	payloads := [][]byte{
		{},
		{0x42},
		{0x00, 0x01, 0x02, 0x03},
		make([]byte, 240),
	}
	for _, payload := range payloads {
		for counter := uint8(0); counter <= 14; counter++ {
			frame, err := p.Protect(payload, counter)
			if err != nil {
				t.Fatalf("Protect(%v, %d): %v", payload, counter, err)
			}
			if len(frame) != e2e.Profile01HeaderSize+len(payload) {
				t.Fatalf("frame len = %d, want %d", len(frame), e2e.Profile01HeaderSize+len(payload))
			}
			got, err := p.Check(frame, counter)
			if err != nil {
				t.Fatalf("Check(counter=%d): %v", counter, err)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("payload mismatch after round-trip")
			}
		}
	}
}

func TestProfile01_CounterOutOfRange(t *testing.T) {
	//fusa:test REQ-E2E-003
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x0001})
	_, err := p.Protect([]byte{0x00}, 15)
	if err == nil {
		t.Error("expected error for counter=15, got nil")
	}
}

func TestProfile01_CRCMismatch(t *testing.T) {
	//fusa:test REQ-E2E-004
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x0001})
	frame, _ := p.Protect([]byte{0xAA, 0xBB}, 3)
	frame[0] ^= 0xFF // corrupt CRC byte
	_, err := p.Check(frame, 3)
	if !errors.Is(err, e2e.ErrCRCMismatch) {
		t.Errorf("expected ErrCRCMismatch, got %v", err)
	}
}

func TestProfile01_CounterMismatch(t *testing.T) {
	//fusa:test REQ-E2E-004
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x0001})
	frame, _ := p.Protect([]byte{0xAA, 0xBB}, 3)
	_, err := p.Check(frame, 4)
	if !errors.Is(err, e2e.ErrCounterMismatch) {
		t.Errorf("expected ErrCounterMismatch, got %v", err)
	}
}

func TestProfile01_ShortFrame(t *testing.T) {
	//fusa:test REQ-E2E-004
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x0001})
	_, err := p.Check([]byte{0x00}, 0) // 1 byte < 2-byte header
	if !errors.Is(err, e2e.ErrShortFrame) {
		t.Errorf("expected ErrShortFrame, got %v", err)
	}
}

func TestProfile01_PayloadCorruption(t *testing.T) {
	//fusa:test REQ-E2E-004
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x5678})
	frame, _ := p.Protect([]byte{0x01, 0x02, 0x03}, 7)
	frame[e2e.Profile01HeaderSize+1] ^= 0x01 // flip one payload bit
	_, err := p.Check(frame, 7)
	if !errors.Is(err, e2e.ErrCRCMismatch) {
		t.Errorf("expected ErrCRCMismatch on payload corruption, got %v", err)
	}
}

func TestProfile01_DifferentDataIDs(t *testing.T) {
	//fusa:test REQ-E2E-003
	//fusa:test REQ-E2E-004
	sender := e2e.NewProfile01(e2e.Profile01Config{DataID: 0xAAAA})
	receiver := e2e.NewProfile01(e2e.Profile01Config{DataID: 0xBBBB})
	frame, _ := sender.Protect([]byte{0x11, 0x22}, 0)
	_, err := receiver.Check(frame, 0)
	// Different DataID changes the CRC — should not match.
	if err == nil {
		t.Error("expected error when DataIDs differ, got nil")
	}
}

func TestProfile01_EmptyPayload(t *testing.T) {
	//fusa:test REQ-E2E-003
	//fusa:test REQ-E2E-004
	p := e2e.NewProfile01(e2e.Profile01Config{DataID: 0x0000})
	frame, err := p.Protect(nil, 0)
	if err != nil {
		t.Fatalf("Protect(nil, 0): %v", err)
	}
	if len(frame) != e2e.Profile01HeaderSize {
		t.Errorf("frame len = %d, want %d", len(frame), e2e.Profile01HeaderSize)
	}
	_, err = p.Check(frame, 0)
	if err != nil {
		t.Errorf("Check empty frame: %v", err)
	}
}

// ── Profile 05 ────────────────────────────────────────────────────────────────

func TestProfile05_RoundTrip(t *testing.T) {
	//fusa:test REQ-E2E-005
	//fusa:test REQ-E2E-006
	//fusa:test REQ-E2E-007
	//fusa:test REQ-E2E-008
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0xAB})
	payloads := [][]byte{
		{},
		{0x42},
		{0x00, 0x01, 0x02, 0x03},
		make([]byte, 4096),
	}
	for i := range payloads[3] {
		payloads[3][i] = byte(i % 251)
	}

	for _, payload := range payloads {
		for _, counter := range []uint8{0, 1, 127, 254, 255} {
			frame, err := p.Protect(payload, counter)
			if err != nil {
				t.Fatalf("Protect(%d bytes, counter=%d): %v", len(payload), counter, err)
			}
			if len(frame) != e2e.Profile05HeaderSize+len(payload) {
				t.Fatalf("frame len = %d, want %d", len(frame), e2e.Profile05HeaderSize+len(payload))
			}
			got, err := p.Check(frame, counter)
			if err != nil {
				t.Fatalf("Check(%d bytes, counter=%d): %v", len(payload), counter, err)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("payload mismatch after round-trip (%d bytes)", len(payload))
			}
		}
	}
}

func TestProfile05_CRCMismatch(t *testing.T) {
	//fusa:test REQ-E2E-008
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x01})
	frame, _ := p.Protect([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 0)
	frame[2] ^= 0xFF // corrupt byte inside CRC field
	_, err := p.Check(frame, 0)
	if !errors.Is(err, e2e.ErrCRCMismatch) {
		t.Errorf("expected ErrCRCMismatch, got %v", err)
	}
}

func TestProfile05_CounterMismatch(t *testing.T) {
	//fusa:test REQ-E2E-008
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x02})
	frame, _ := p.Protect([]byte{0xCA, 0xFE}, 10)
	_, err := p.Check(frame, 11)
	if !errors.Is(err, e2e.ErrCounterMismatch) {
		t.Errorf("expected ErrCounterMismatch, got %v", err)
	}
}

func TestProfile05_ShortFrame(t *testing.T) {
	//fusa:test REQ-E2E-008
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x03})
	_, err := p.Check(make([]byte, 7), 0) // 7 < 8-byte header
	if !errors.Is(err, e2e.ErrShortFrame) {
		t.Errorf("expected ErrShortFrame, got %v", err)
	}
}

func TestProfile05_PayloadCorruption(t *testing.T) {
	//fusa:test REQ-E2E-007
	//fusa:test REQ-E2E-008
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x04})
	frame, _ := p.Protect([]byte{0x01, 0x02, 0x03, 0x04, 0x05}, 5)
	frame[e2e.Profile05HeaderSize+3] ^= 0x01 // flip one payload bit
	_, err := p.Check(frame, 5)
	if !errors.Is(err, e2e.ErrCRCMismatch) {
		t.Errorf("expected ErrCRCMismatch on payload corruption, got %v", err)
	}
}

func TestProfile05_DifferentDataIDs(t *testing.T) {
	//fusa:test REQ-E2E-007
	//fusa:test REQ-E2E-008
	sender := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x10})
	receiver := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x20})
	frame, _ := sender.Protect([]byte{0x11, 0x22}, 0)
	_, err := receiver.Check(frame, 0)
	if err == nil {
		t.Error("expected error when DataIDs differ, got nil")
	}
}

func TestProfile05_EmptyPayload(t *testing.T) {
	//fusa:test REQ-E2E-007
	//fusa:test REQ-E2E-008
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0x00})
	frame, err := p.Protect(nil, 255)
	if err != nil {
		t.Fatalf("Protect(nil, 255): %v", err)
	}
	if len(frame) != e2e.Profile05HeaderSize {
		t.Errorf("frame len = %d, want %d", len(frame), e2e.Profile05HeaderSize)
	}
	_, err = p.Check(frame, 255)
	if err != nil {
		t.Errorf("Check empty frame: %v", err)
	}
}

func TestProfile05_ReservedBytesZero(t *testing.T) {
	//fusa:test REQ-E2E-007
	p := e2e.NewProfile05(e2e.Profile05Config{DataID: 0xFF})
	frame, _ := p.Protect([]byte{0x01}, 0)
	// Verify reserved bytes 5-7 are zero.
	for i := 5; i < 8; i++ {
		if frame[i] != 0x00 {
			t.Errorf("reserved byte %d = 0x%02x, want 0x00", i, frame[i])
		}
	}
}

// ── Fuzz ─────────────────────────────────────────────────────────────────────

// FuzzProfile01RoundTrip verifies that Protect→Check always succeeds for valid inputs.
func FuzzProfile01RoundTrip(f *testing.F) {
	//fusa:test REQ-E2E-003
	//fusa:test REQ-E2E-004
	f.Add(uint16(0x1234), []byte{0xAA, 0xBB}, uint8(3))
	f.Add(uint16(0x0000), []byte{}, uint8(0))
	f.Add(uint16(0xFFFF), []byte{0x01, 0x02, 0x03, 0x04, 0x05}, uint8(14))

	f.Fuzz(func(t *testing.T, dataID uint16, payload []byte, counter uint8) {
		if counter > 14 {
			t.Skip()
		}
		p := e2e.NewProfile01(e2e.Profile01Config{DataID: dataID})
		frame, err := p.Protect(payload, counter)
		if err != nil {
			t.Fatalf("Protect: %v", err)
		}
		got, err := p.Check(frame, counter)
		if err != nil {
			t.Fatalf("Check after Protect: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Error("payload mismatch")
		}
	})
}

// FuzzProfile05RoundTrip verifies that Protect→Check always succeeds for valid inputs.
func FuzzProfile05RoundTrip(f *testing.F) {
	//fusa:test REQ-E2E-007
	//fusa:test REQ-E2E-008
	f.Add(uint8(0xAB), []byte{0xDE, 0xAD, 0xBE, 0xEF}, uint8(0))
	f.Add(uint8(0x00), []byte{}, uint8(255))
	f.Add(uint8(0xFF), []byte{0x01}, uint8(127))

	f.Fuzz(func(t *testing.T, dataID uint8, payload []byte, counter uint8) {
		p := e2e.NewProfile05(e2e.Profile05Config{DataID: dataID})
		frame, err := p.Protect(payload, counter)
		if err != nil {
			t.Fatalf("Protect: %v", err)
		}
		got, err := p.Check(frame, counter)
		if err != nil {
			t.Fatalf("Check after Protect: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Error("payload mismatch")
		}
	})
}
