// Package socket implements the Unix domain socket transport layer for
// agent2shell, including length-prefixed JSON framing and socket lifecycle
// helpers.
package socket

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxFrameSize is the maximum allowed payload size in bytes (16 MiB).
// ReadFrame returns ErrFrameTooLarge when the encoded length exceeds this
// value.
const MaxFrameSize = 16 * 1024 * 1024

// ErrFrameTooLarge is returned by ReadFrame when the 4-byte length header
// encodes a value larger than MaxFrameSize.
var ErrFrameTooLarge = errors.New("frame too large")

// WriteFrame marshals v to JSON, prepends a 4-byte big-endian length header,
// and writes both to w in a single logical operation.
func WriteFrame(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("WriteFrame: marshal JSON: %w", err)
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("WriteFrame: write length header: %w", err)
	}

	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("WriteFrame: write payload: %w", err)
	}

	return nil
}

// ReadFrame reads a length-prefixed JSON frame from r and unmarshals the
// payload into v.
//
// Protocol:
//  1. Read exactly 4 bytes (big-endian uint32) as the payload length.
//  2. Reject frames larger than MaxFrameSize — returns ErrFrameTooLarge.
//  3. Read exactly that many bytes.
//  4. Unmarshal JSON into v.
//
// io.ReadFull is used for all reads; bufio.Reader is intentionally avoided so
// that no bytes are consumed beyond the current frame.
func ReadFrame(r io.Reader, v any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return fmt.Errorf("ReadFrame: read length header: %w", err)
	}

	length := binary.BigEndian.Uint32(header[:])
	if length > MaxFrameSize {
		return ErrFrameTooLarge
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return fmt.Errorf("ReadFrame: read payload: %w", err)
	}

	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("ReadFrame: unmarshal JSON: %w", err)
	}

	return nil
}
