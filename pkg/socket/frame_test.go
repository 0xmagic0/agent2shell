package socket_test

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// S4.1 — WriteFrame + ReadFrame round-trip via io.Pipe.
func TestWriteReadFrame_RoundTrip(t *testing.T) {
	req := types.Request{
		Type:    types.RunRequest,
		Command: "id",
		Timeout: 30,
	}

	pr, pw := io.Pipe()

	// Write in a goroutine so the pipe does not deadlock.
	errCh := make(chan error, 1)
	go func() {
		errCh <- socket.WriteFrame(pw, req)
		pw.Close()
	}()

	var got types.Request
	err := socket.ReadFrame(pr, &got)
	require.NoError(t, err)

	writeErr := <-errCh
	require.NoError(t, writeErr)

	assert.Equal(t, req.Type, got.Type)
	assert.Equal(t, req.Command, got.Command)
	assert.Equal(t, req.Timeout, got.Timeout)
}

// S4.2 — ReadFrame rejects a frame whose length header exceeds MaxFrameSize.
func TestReadFrame_OversizedFrame(t *testing.T) {
	pr, pw := io.Pipe()

	go func() {
		// Write a length header of MaxFrameSize+1 with no payload.
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(socket.MaxFrameSize)+1)
		pw.Write(header[:]) //nolint:errcheck
		pw.Close()
	}()

	var v any
	err := socket.ReadFrame(pr, &v)
	require.Error(t, err)
	assert.True(t, errors.Is(err, socket.ErrFrameTooLarge),
		"expected ErrFrameTooLarge, got %v", err)
}

// S4.3 — ReadFrame handles a reader that delivers one byte at a time.
func TestReadFrame_FragmentedDelivery(t *testing.T) {
	req := types.Request{
		Type:    types.RunRequest,
		Command: "whoami",
		Timeout: 10,
	}

	pr, pw := io.Pipe()

	errCh := make(chan error, 1)
	go func() {
		errCh <- socket.WriteFrame(pw, req)
		pw.Close()
	}()

	// Wrap the pipe reader in a one-byte-at-a-time reader.
	slow := &oneByteReader{r: pr}

	var got types.Request
	err := socket.ReadFrame(slow, &got)
	require.NoError(t, err)

	writeErr := <-errCh
	require.NoError(t, writeErr)

	assert.Equal(t, req.Type, got.Type)
	assert.Equal(t, req.Command, got.Command)
}

// oneByteReader wraps an io.Reader and delivers exactly one byte per Read call.
type oneByteReader struct{ r io.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}
