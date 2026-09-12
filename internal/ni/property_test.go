// SPDX-License-Identifier: Apache-2.0
//
// Ported from the NI cases of open-rfc test/protocol-property.test.ts at commit
// 847036d, Copyright 2026 Marian Zeis, licensed under the Apache License,
// Version 2.0. Modified by open-rfc-go contributors: rewritten for the testing
// package, with math/rand/v2 PCG replacing upstream's hand-rolled
// DeterministicRandom — the generated sequences therefore differ, while the
// invariants asserted are the same. The fuzz targets have no upstream
// counterpart. See docs/provenance.md.

package ni

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
)

const (
	chunkingRuns = 256
	hostileRuns  = 512
)

// A frame survives any chunking of the stream that carries it.
func TestPropertyFramesSurviveArbitraryStreamChunking(t *testing.T) {
	random := rand.New(rand.NewPCG(0x4f50454e, 0x52464321))

	for run := range chunkingRuns {
		payloads := make([][]byte, 1+random.IntN(8))
		var stream []byte
		for index := range payloads {
			payload := make([]byte, random.IntN(2049))
			for byteIndex := range payload {
				payload[byteIndex] = byte(random.UintN(256))
			}
			payloads[index] = payload
			stream = append(stream, mustEncode(t, payload)...)
		}

		decoder := mustDecoder(t, 2048)
		var decoded [][]byte
		for offset := 0; offset < len(stream); {
			length := min(len(stream)-offset, 1+random.IntN(97))
			produced, err := decoder.Push(stream[offset : offset+length])
			if err != nil {
				t.Fatalf("run %d: Push returned %v", run, err)
			}
			decoded = append(decoded, produced...)
			offset += length
		}
		if err := decoder.Finish(); err != nil {
			t.Fatalf("run %d: Finish returned %v", run, err)
		}

		if len(decoded) != len(payloads) {
			t.Fatalf("run %d: decoded %d payloads, want %d", run, len(decoded), len(payloads))
		}
		for index := range payloads {
			if !bytes.Equal(decoded[index], payloads[index]) {
				t.Fatalf("run %d: payload %d did not survive chunking", run, index)
			}
		}
		if decoder.Buffered() != 0 {
			t.Fatalf("run %d: Buffered() = %d, want 0", run, decoder.Buffered())
		}
	}
}

// An arbitrary hostile prefix is either rejected, retained, or decoded within
// bounds — never anything else, and never a panic.
func TestPropertyBoundedDecoderRejectsOrRetainsHostilePrefixes(t *testing.T) {
	random := rand.New(rand.NewPCG(0x52464321, 0x4f50454e))

	for run := range hostileRuns {
		input := make([]byte, random.IntN(65))
		for index := range input {
			input[index] = byte(random.UintN(256))
		}

		decoder := mustDecoder(t, 4096)
		frames, err := decoder.Push(input)
		if err != nil {
			if !errors.Is(err, ErrPayloadTooLarge) {
				t.Fatalf("run %d: Push returned %v, want ErrPayloadTooLarge", run, err)
			}
			continue
		}
		for _, frame := range frames {
			if len(frame) > 4096 {
				t.Fatalf("run %d: decoded a %d-byte frame above the 4096 limit", run, len(frame))
			}
		}
		if decoder.Buffered() > len(input) {
			t.Fatalf("run %d: Buffered() = %d exceeds the %d bytes pushed",
				run, decoder.Buffered(), len(input))
		}
		if err := decoder.Finish(); err != nil && !errors.Is(err, ErrTruncatedStream) {
			t.Fatalf("run %d: Finish returned %v, want nil or ErrTruncatedStream", run, err)
		}
	}
}

// The decoder consumes network bytes, so it gets a fuzz target. The assertion
// is deliberately weak — bounds hold, nothing panics — because for arbitrary
// input there is no expected output to compare against.
func FuzzFrameDecoderBounds(f *testing.F) {
	f.Add([]byte{0, 0, 0, 3, 0x52, 0x46, 0x43})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{})

	const limit = 4096
	f.Fuzz(func(t *testing.T, input []byte) {
		decoder := mustDecoder(t, limit)
		frames, err := decoder.Push(input)
		if err != nil {
			if !errors.Is(err, ErrPayloadTooLarge) {
				t.Fatalf("Push returned %v, want ErrPayloadTooLarge", err)
			}
			return
		}
		for _, frame := range frames {
			if len(frame) > limit {
				t.Fatalf("decoded a %d-byte frame above the %d limit", len(frame), limit)
			}
		}
		if decoder.Buffered() > len(input) {
			t.Fatalf("Buffered() = %d exceeds the %d bytes pushed", decoder.Buffered(), len(input))
		}
		decoder.Reset()
		if decoder.Buffered() != 0 {
			t.Fatalf("Buffered() = %d after Reset, want 0", decoder.Buffered())
		}
	})
}

// Whatever EncodeFrame produces, the decoder returns unchanged, at any split.
func FuzzFrameRoundTrip(f *testing.F) {
	f.Add([]byte("RFC"), 1)
	f.Add([]byte{}, 3)
	f.Add(bytes.Repeat([]byte{0xa5}, 300), 7)

	f.Fuzz(func(t *testing.T, payload []byte, split int) {
		frame, err := EncodeFrame(payload)
		if err != nil {
			t.Fatalf("EncodeFrame returned %v", err)
		}
		decoder := mustDecoder(t, DefaultMaxPayloadLength)

		width := 1
		if split > 0 {
			width = split%len(frame) + 1
		}

		var decoded [][]byte
		for offset := 0; offset < len(frame); offset += width {
			produced, err := decoder.Push(frame[offset:min(offset+width, len(frame))])
			if err != nil {
				t.Fatalf("Push returned %v", err)
			}
			decoded = append(decoded, produced...)
		}
		if err := decoder.Finish(); err != nil {
			t.Fatalf("Finish returned %v", err)
		}

		if len(decoded) != 1 {
			t.Fatalf("decoded %d payloads, want 1", len(decoded))
		}
		if !bytes.Equal(decoded[0], payload) {
			t.Fatalf("round trip changed a %d-byte payload", len(payload))
		}
	})
}
