// SPDX-License-Identifier: Apache-2.0
//
// Ported from open-rfc test/ni.test.ts at commit 847036d,
// Copyright 2026 Marian Zeis, licensed under the Apache License, Version 2.0.
// Modified by open-rfc-go contributors: rewritten for the testing package.
// Upstream's "encodes from intrinsic typed-array geometry" case has no Go
// analogue — Go slices expose no user-overridable geometry — and is replaced by
// the two aliasing tests below, which cover the same intent.
// See docs/provenance.md.

package ni

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func mustEncode(t *testing.T, payload []byte) []byte {
	t.Helper()
	frame, err := EncodeFrame(payload)
	if err != nil {
		t.Fatalf("EncodeFrame(%d bytes) returned %v", len(payload), err)
	}
	return frame
}

func mustDecoder(t *testing.T, maxPayloadLength int) *FrameDecoder {
	t.Helper()
	decoder, err := NewFrameDecoder(maxPayloadLength)
	if err != nil {
		t.Fatalf("NewFrameDecoder(%d) returned %v", maxPayloadLength, err)
	}
	return decoder
}

func TestEncodeFrameWritesAFourByteBigEndianPayloadLength(t *testing.T) {
	got := hex.EncodeToString(mustEncode(t, []byte("RFC")))
	if want := "00000003524643"; got != want {
		t.Fatalf("frame = %s, want %s", got, want)
	}
}

func TestEncodeFrameEncodesAnEmptyPayloadAsABareLength(t *testing.T) {
	got := hex.EncodeToString(mustEncode(t, nil))
	if want := "00000000"; got != want {
		t.Fatalf("frame = %s, want %s", got, want)
	}
}

// The Go counterpart of upstream's hostile-geometry test: the frame must not
// observe later mutations of the caller's slice.
func TestEncodeFrameDoesNotAliasCallerPayload(t *testing.T) {
	payload := []byte("RFC")
	frame := mustEncode(t, payload)
	payload[0] = 0xff

	if got, want := hex.EncodeToString(frame), "00000003524643"; got != want {
		t.Fatalf("frame = %s after mutating the caller payload, want %s", got, want)
	}
}

func TestPushDoesNotAliasCallerChunk(t *testing.T) {
	chunk := mustEncode(t, []byte("first"))
	decoder := mustDecoder(t, DefaultMaxPayloadLength)

	payloads, err := decoder.Push(chunk)
	if err != nil {
		t.Fatalf("Push returned %v", err)
	}
	clear(chunk)

	if len(payloads) != 1 || !bytes.Equal(payloads[0], []byte("first")) {
		t.Fatalf("payloads = %q after clearing the caller chunk, want [\"first\"]", payloads)
	}
}

func TestDecodesFragmentedAndCoalescedFrames(t *testing.T) {
	wire := append(mustEncode(t, []byte("first")), mustEncode(t, []byte("second"))...)
	decoder := mustDecoder(t, DefaultMaxPayloadLength)

	var decoded []string
	for index := range wire {
		payloads, err := decoder.Push(wire[index : index+1])
		if err != nil {
			t.Fatalf("Push at byte %d returned %v", index, err)
		}
		for _, payload := range payloads {
			decoded = append(decoded, string(payload))
		}
	}
	if err := decoder.Finish(); err != nil {
		t.Fatalf("Finish returned %v", err)
	}

	if len(decoded) != 2 || decoded[0] != "first" || decoded[1] != "second" {
		t.Fatalf("decoded = %q, want [first second]", decoded)
	}
}

func TestDecodesTwoFramesFromOneChunk(t *testing.T) {
	wire := append(mustEncode(t, []byte("first")), mustEncode(t, []byte("second"))...)
	decoder := mustDecoder(t, DefaultMaxPayloadLength)

	payloads, err := decoder.Push(wire)
	if err != nil {
		t.Fatalf("Push returned %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("len(payloads) = %d, want 2", len(payloads))
	}
	if got := string(payloads[0]); got != "first" {
		t.Fatalf("payloads[0] = %q, want \"first\"", got)
	}
	if got := string(payloads[1]); got != "second" {
		t.Fatalf("payloads[1] = %q, want \"second\"", got)
	}
	if decoder.Buffered() != 0 {
		t.Fatalf("Buffered() = %d, want 0", decoder.Buffered())
	}
}

func TestDecodesALargeFrameDeliveredOneByteAtATime(t *testing.T) {
	expected := bytes.Repeat([]byte{0xa5}, 64*1024)
	wire := mustEncode(t, expected)
	decoder := mustDecoder(t, len(expected))

	var decoded []byte
	for index := range wire {
		payloads, err := decoder.Push(wire[index : index+1])
		if err != nil {
			t.Fatalf("Push at byte %d returned %v", index, err)
		}
		if len(payloads) > 0 {
			decoded = payloads[0]
		}
	}
	if err := decoder.Finish(); err != nil {
		t.Fatalf("Finish returned %v", err)
	}

	if !bytes.Equal(decoded, expected) {
		t.Fatalf("decoded %d bytes, want %d equal bytes", len(decoded), len(expected))
	}
	if decoder.Buffered() != 0 {
		t.Fatalf("Buffered() = %d, want 0", decoder.Buffered())
	}
}

// Exercises the queue-compaction branch that keeps a long-lived connection from
// growing its chunk list without bound: many consumed chunks, with a partial
// header still buffered behind them.
func TestCompactsConsumedChunksWhileRetainingAPartialHeader(t *testing.T) {
	// A 104-byte frame followed by three bytes of the next frame's header. The
	// trailing bytes arrive in the same chunk as the frame's last byte, so the
	// queue still holds an unconsumed chunk when compaction runs — which is the
	// branch this test exists for.
	wire := append(mustEncode(t, bytes.Repeat([]byte{0x5a}, 100)), 0x00, 0x00, 0x00)
	decoder := mustDecoder(t, DefaultMaxPayloadLength)

	var decoded [][]byte
	for index := range 103 {
		payloads, err := decoder.Push(wire[index : index+1])
		if err != nil {
			t.Fatalf("Push at byte %d returned %v", index, err)
		}
		decoded = append(decoded, payloads...)
	}
	payloads, err := decoder.Push(wire[103:])
	if err != nil {
		t.Fatalf("final Push returned %v", err)
	}
	decoded = append(decoded, payloads...)

	if len(decoded) != 1 || len(decoded[0]) != 100 {
		t.Fatalf("decoded %d payloads, want 1 of 100 bytes", len(decoded))
	}
	if decoder.Buffered() != 3 {
		t.Fatalf("Buffered() = %d, want 3", decoder.Buffered())
	}
	if got := len(decoder.chunks); got != 1 {
		t.Fatalf("retained chunks = %d after compaction, want 1", got)
	}
	if decoder.headIndex != 0 || decoder.headOffset != 1 {
		t.Fatalf("head = (%d, %d) after compaction, want (0, 1)",
			decoder.headIndex, decoder.headOffset)
	}

	// The decoder must still be usable across the compaction boundary.
	payloads, err = decoder.Push([]byte{0x02, 0x41, 0x42})
	if err != nil {
		t.Fatalf("Push after compaction returned %v", err)
	}
	if len(payloads) != 1 || string(payloads[0]) != "AB" {
		t.Fatalf("payloads = %q, want [\"AB\"]", payloads)
	}
}

func TestRejectsAnAdvertisedPayloadAboveTheConfiguredLimit(t *testing.T) {
	decoder := mustDecoder(t, 4)

	_, err := decoder.Push([]byte{0, 0, 0, 5})
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Push returned %v, want ErrPayloadTooLarge", err)
	}
}

func TestAcceptsAnAdvertisedPayloadExactlyAtTheConfiguredLimit(t *testing.T) {
	decoder := mustDecoder(t, 4)

	payloads, err := decoder.Push([]byte{0, 0, 0, 4, 'a', 'b', 'c', 'd'})
	if err != nil {
		t.Fatalf("Push returned %v", err)
	}
	if len(payloads) != 1 || string(payloads[0]) != "abcd" {
		t.Fatalf("payloads = %q, want [\"abcd\"]", payloads)
	}
}

func TestReportsATruncatedStream(t *testing.T) {
	decoder := mustDecoder(t, DefaultMaxPayloadLength)

	if _, err := decoder.Push([]byte{0, 0, 0, 3, 0x52}); err != nil {
		t.Fatalf("Push returned %v", err)
	}
	if err := decoder.Finish(); !errors.Is(err, ErrTruncatedStream) {
		t.Fatalf("Finish returned %v, want ErrTruncatedStream", err)
	}

	decoder.Reset()
	if decoder.Buffered() != 0 {
		t.Fatalf("Buffered() = %d after Reset, want 0", decoder.Buffered())
	}
	if err := decoder.Finish(); err != nil {
		t.Fatalf("Finish after Reset returned %v", err)
	}
}

func TestResetZeroesRetainedBytes(t *testing.T) {
	decoder := mustDecoder(t, DefaultMaxPayloadLength)
	if _, err := decoder.Push([]byte{0, 0, 0, 8, 's', 'e', 'c', 'r', 'e', 't'}); err != nil {
		t.Fatalf("Push returned %v", err)
	}

	retained := decoder.chunks[0]
	decoder.Reset()

	if !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatalf("retained chunk = % x after Reset, want all zero", retained)
	}
}

func TestNewFrameDecoderRejectsANegativeLimit(t *testing.T) {
	if _, err := NewFrameDecoder(-1); !errors.Is(err, ErrNegativeLimit) {
		t.Fatalf("NewFrameDecoder(-1) returned %v, want ErrNegativeLimit", err)
	}
}

func TestAZeroLimitAcceptsOnlyEmptyPayloads(t *testing.T) {
	decoder := mustDecoder(t, 0)

	payloads, err := decoder.Push([]byte{0, 0, 0, 0})
	if err != nil {
		t.Fatalf("Push returned %v", err)
	}
	if len(payloads) != 1 || len(payloads[0]) != 0 {
		t.Fatalf("payloads = %q, want one empty payload", payloads)
	}

	if _, err := decoder.Push([]byte{0, 0, 0, 1}); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Push returned %v, want ErrPayloadTooLarge", err)
	}
}
