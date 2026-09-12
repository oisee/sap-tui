// SPDX-License-Identifier: Apache-2.0
//
// Ported from open-rfc src/protocol/ni.ts at commit 847036d,
// Copyright 2026 Marian Zeis, licensed under the Apache License, Version 2.0.
// Modified by open-rfc-go contributors: rewritten in Go. Thrown errors became
// returned wrapped sentinels, and the typed-array geometry guards of
// src/protocol/bytes.ts were dropped as inapplicable to Go slices.
// See docs/provenance.md.

// Package ni separates the four-byte, big-endian length-prefixed records used
// by SAP's Network Interface layer.
//
// The layer carries no semantics of its own: it delimits records and nothing
// else. Everything above it — APPC, CPIC, the RFC conversation — sees whole
// payloads and never a partial read.
package ni

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// lengthBytes is the width of the NI length prefix. It is not exported: a
// caller that needs to know the framing width is reaching through this package
// rather than using it.
const lengthBytes = 4

// DefaultMaxPayloadLength bounds a single decoded NI payload.
//
// The bound exists because the length prefix is attacker-controlled: a peer can
// advertise up to 4 GiB and a decoder that trusts it allocates on demand.
const DefaultMaxPayloadLength = 256 * 1024 * 1024

var (
	// ErrPayloadTooLong reports a payload that cannot be described by the
	// unsigned 32-bit length field at all.
	ErrPayloadTooLong = errors.New("ni: payload exceeds the unsigned 32-bit length field")

	// ErrPayloadTooLarge reports an advertised payload length above the
	// decoder's configured limit. The peer may be hostile or misconfigured;
	// either way the stream is no longer trustworthy and the decoder must be
	// discarded rather than reused.
	ErrPayloadTooLarge = errors.New("ni: advertised payload exceeds the configured limit")

	// ErrTruncatedStream reports bytes still buffered when the stream ended,
	// which means the peer closed mid-frame.
	ErrTruncatedStream = errors.New("ni: truncated stream")

	// ErrNegativeLimit reports an invalid decoder configuration.
	ErrNegativeLimit = errors.New("ni: maximum payload length must not be negative")

	// ErrInconsistentQueue reports that the decoder's internal accounting
	// disagrees with its buffers. It is unreachable by construction and is
	// returned rather than panicked so that a library consumer can survive it.
	ErrInconsistentQueue = errors.New("ni: decoder queue is inconsistent")
)

// EncodeFrame returns payload prefixed with its four-byte, big-endian length.
//
// The returned frame never aliases payload, so the caller may reuse or mutate
// its slice immediately.
func EncodeFrame(payload []byte) ([]byte, error) {
	if uint64(len(payload)) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: %d bytes", ErrPayloadTooLong, len(payload))
	}

	frame := make([]byte, lengthBytes+len(payload))
	binary.BigEndian.PutUint32(frame[:lengthBytes], uint32(len(payload)))
	copy(frame[lengthBytes:], payload)
	return frame, nil
}

// FrameDecoder incrementally separates NI frames from an arbitrarily chunked
// byte stream. A FrameDecoder is not safe for concurrent use; the connection
// that owns the socket owns its decoder.
type FrameDecoder struct {
	maxPayloadLength int

	// chunks holds retained copies of pushed data. headIndex/headOffset mark
	// the first unconsumed byte, so consuming does not shift the queue.
	chunks     [][]byte
	headIndex  int
	headOffset int
	buffered   int
}

// NewFrameDecoder returns a decoder bounded to maxPayloadLength bytes per
// payload. Pass DefaultMaxPayloadLength unless a route imposes something
// tighter.
//
// The bound is a limit, not a promise about the protocol: a real payload may be
// any length the peer's release permits, and a decoder that rejects a
// structurally valid frame because one system never sent one that big is the
// bug described in docs/recurring-bug-class.md.
func NewFrameDecoder(maxPayloadLength int) (*FrameDecoder, error) {
	if maxPayloadLength < 0 {
		return nil, fmt.Errorf("%w: %d", ErrNegativeLimit, maxPayloadLength)
	}
	return &FrameDecoder{maxPayloadLength: maxPayloadLength}, nil
}

// Buffered reports the retained bytes of an incomplete frame.
func (d *FrameDecoder) Buffered() int { return d.buffered }

// Push adds chunk to the stream and returns every complete payload it
// completes, in order. chunk is copied, so the caller may reuse its read
// buffer immediately.
//
// On error no payloads are returned even if some were decoded before the fault,
// because a decoder that has seen a length it cannot trust has lost stream
// synchronisation: the bytes it would hand back may be framing rather than
// payload. Discard the decoder and the connection.
func (d *FrameDecoder) Push(chunk []byte) ([][]byte, error) {
	if len(chunk) > 0 {
		retained := make([]byte, len(chunk))
		copy(retained, chunk)
		d.chunks = append(d.chunks, retained)
		d.buffered += len(chunk)
	}

	var payloads [][]byte
	for d.buffered >= lengthBytes {
		advertised, err := d.peekPayloadLength()
		if err != nil {
			return nil, err
		}
		if uint64(advertised) > uint64(d.maxPayloadLength) {
			return nil, fmt.Errorf("%w: %d exceeds %d",
				ErrPayloadTooLarge, advertised, d.maxPayloadLength)
		}
		// Compared as uint64: on a 32-bit platform lengthBytes+advertised
		// overflows int, and the frame is simply not complete yet.
		if uint64(d.buffered) < uint64(lengthBytes)+uint64(advertised) {
			break
		}

		if _, err := d.consume(lengthBytes); err != nil {
			return nil, err
		}
		// advertised is <= maxPayloadLength, an int, so the conversion is safe.
		payload, err := d.consume(int(advertised))
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

// Finish reports whether the stream ended on a frame boundary. It leaves the
// decoder's buffers intact; call Reset to release them.
func (d *FrameDecoder) Finish() error {
	if d.buffered != 0 {
		return fmt.Errorf("%w: %d bytes remain", ErrTruncatedStream, d.buffered)
	}
	return nil
}

// Reset releases and zeroes every retained partial-frame byte.
//
// The zeroing is deliberate: a partial frame can hold credential material, and
// a pooled connection's decoder outlives the call that filled it. Go's garbage
// collector may still have moved copies of those bytes, so this narrows the
// window rather than closing it.
func (d *FrameDecoder) Reset() {
	d.wipe(0, len(d.chunks))
	d.chunks = nil
	d.headIndex = 0
	d.headOffset = 0
	d.buffered = 0
}

// peekPayloadLength reads the length prefix without consuming it. The prefix
// may straddle any number of pushed chunks.
func (d *FrameDecoder) peekPayloadLength() (uint32, error) {
	var value uint32
	remaining := lengthBytes
	chunkIndex := d.headIndex
	chunkOffset := d.headOffset

	for remaining > 0 {
		if chunkIndex >= len(d.chunks) {
			return 0, ErrInconsistentQueue
		}
		current := d.chunks[chunkIndex]
		take := min(remaining, len(current)-chunkOffset)
		for _, b := range current[chunkOffset : chunkOffset+take] {
			value = value<<8 | uint32(b)
		}
		remaining -= take
		chunkIndex++
		chunkOffset = 0
	}
	return value, nil
}

// consume removes length bytes from the head of the queue and returns them as
// a fresh slice that does not alias the retained chunks.
func (d *FrameDecoder) consume(length int) ([]byte, error) {
	if length > d.buffered {
		return nil, ErrInconsistentQueue
	}

	result := make([]byte, length)
	written := 0
	for written < length {
		if d.headIndex >= len(d.chunks) {
			return nil, ErrInconsistentQueue
		}
		current := d.chunks[d.headIndex]
		take := min(length-written, len(current)-d.headOffset)
		copy(result[written:], current[d.headOffset:d.headOffset+take])
		written += take
		d.headOffset += take
		if d.headOffset == len(current) {
			d.headIndex++
			d.headOffset = 0
		}
	}

	d.buffered -= length
	d.compact()
	return result, nil
}

// compact drops fully consumed chunks. It runs on consumption rather than on a
// timer so that a long-lived connection decoding many small frames does not
// grow its queue without bound.
func (d *FrameDecoder) compact() {
	if d.headIndex == len(d.chunks) {
		d.wipe(0, len(d.chunks))
		d.chunks = nil
		d.headIndex = 0
		return
	}
	if d.headIndex >= 64 && d.headIndex*2 >= len(d.chunks) {
		d.wipe(0, d.headIndex)
		// Copied into a fresh slice rather than resliced: reslicing keeps the
		// old backing array, and with it the pointers to the consumed chunks
		// this just zeroed, alive for as long as the decoder lives.
		remaining := make([][]byte, len(d.chunks)-d.headIndex)
		copy(remaining, d.chunks[d.headIndex:])
		d.chunks = remaining
		d.headIndex = 0
	}
}

// wipe zeroes and releases chunks in [from, to).
func (d *FrameDecoder) wipe(from, to int) {
	for index := from; index < to; index++ {
		clear(d.chunks[index])
		d.chunks[index] = nil
	}
}
