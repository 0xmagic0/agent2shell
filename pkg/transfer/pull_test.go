package transfer

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pullMockExec returns an ExecFunc that dispatches based on command substrings.
// handlers is a slice of (substring, response) pairs checked in order.
// If no handler matches, returns exit 1 with the unexpected command.
func pullMockExec(handlers []pullHandler) ExecFunc {
	return func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		for _, h := range handlers {
			if strings.Contains(cmd, h.substr) {
				if h.err != nil {
					return nil, h.err
				}
				return h.resp, nil
			}
		}
		return &types.ExecResponse{ExitCode: 1, Output: "unexpected command: " + cmd}, nil
	}
}

type pullHandler struct {
	substr string
	resp   *types.ExecResponse
	err    error
}

// hexMD5 returns the hex-encoded MD5 of data (for building test expectations).
func hexMD5(data []byte) string {
	h := md5.Sum(data)
	return fmt.Sprintf("%x", h)
}

// testEncoder returns an Encoder using the standard base64 binary for tests.
func testEncoder() *Encoder {
	return &Encoder{Name: "base64", Command: "base64"}
}

// TestPull_NilEncoderReturnsError verifies Pull returns ErrNoEncoder when opts.Encoder is nil.
func TestPull_NilEncoderReturnsError(t *testing.T) {
	t.Parallel()

	var calls int
	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		calls++
		return &types.ExecResponse{ExitCode: 0, Output: "100\n"}, nil
	}

	localPath := filepath.Join(t.TempDir(), "out.txt")
	err := Pull(context.Background(), exec, "/remote/file.txt", localPath, PullOpts{
		Encoder:     nil, // explicitly nil
		Checksummer: testChecksummer,
	})

	require.ErrorIs(t, err, ErrNoEncoder)
	assert.Equal(t, 0, calls, "no exec calls expected when encoder is nil")
}

// TestPull_NilChecksummerSkipsVerification verifies Pull completes successfully
// when opts.Checksummer is nil — no checksum step, no error.
func TestPull_NilChecksummerSkipsVerification(t *testing.T) {
	t.Parallel()

	content := []byte("data without checksum verification")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := len(content)

	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", size)}, nil
		}
		// base64 encoder command
		if strings.Contains(cmd, "base64") {
			return &types.ExecResponse{Output: encoded + "\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1, Output: "unexpected: " + cmd}, nil
	}

	localPath := filepath.Join(t.TempDir(), "no_checksum.txt")
	err := Pull(context.Background(), exec, "/remote/file.txt", localPath, PullOpts{
		Encoder:     testEncoder(),
		Checksummer: nil, // nil → skip verification, no error
	})

	require.NoError(t, err)
	got, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// TestPull_SmallFileUsesEncoderCommand verifies pullSmall uses opts.Encoder.Command,
// not a hardcoded "base64" binary.
func TestPull_SmallFileUsesEncoderCommand(t *testing.T) {
	t.Parallel()

	content := []byte("small file content")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := len(content)
	hash := hexMD5(content)

	customEncoder := &Encoder{Name: "openssl", Command: "openssl enc -base64"}
	var capturedBase64Cmd string

	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", size)}, nil
		}
		if strings.Contains(cmd, "openssl enc -base64") || strings.Contains(cmd, "base64") {
			capturedBase64Cmd = cmd
			return &types.ExecResponse{Output: encoded + "\n"}, nil
		}
		if strings.Contains(cmd, "md5sum") {
			return &types.ExecResponse{Output: hash + "\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1, Output: "unexpected: " + cmd}, nil
	}

	localPath := filepath.Join(t.TempDir(), "encoder_cmd.txt")
	err := Pull(context.Background(), exec, "/remote/file.txt", localPath, PullOpts{
		Encoder:     customEncoder,
		Checksummer: testChecksummer,
	})

	require.NoError(t, err)
	assert.Contains(t, capturedBase64Cmd, "openssl enc -base64",
		"pullSmall must use encoder.Command, not hardcoded base64; got: %s", capturedBase64Cmd)
	assert.NotContains(t, capturedBase64Cmd, "base64 <",
		"must NOT use hardcoded 'base64 <' form")
}

// TestPull_ChunkedUsesEncoderCommand verifies pullChunked uses opts.Encoder.Command.
func TestPull_ChunkedUsesEncoderCommand(t *testing.T) {
	t.Parallel()

	chunkSize := 6 * 1024 * 1024
	totalSize := int64(smallFileThreshold + 1024*1024) // 17 MB → chunked path

	content := make([]byte, totalSize)
	for i := range content {
		content[i] = byte(i % 256)
	}

	numChunks := int((totalSize + int64(chunkSize) - 1) / int64(chunkSize))
	chunkEncoded := make([]string, numChunks)
	for i := 0; i < numChunks; i++ {
		offset := int64(i) * int64(chunkSize)
		end := offset + int64(chunkSize)
		if end > totalSize {
			end = totalSize
		}
		chunkEncoded[i] = base64.StdEncoding.EncodeToString(content[offset:end])
	}

	chunkIdx := 0
	customEncoder := &Encoder{Name: "openssl", Command: "openssl enc -base64"}
	var capturedDDCmd string

	hash := hexMD5(content)

	exec := func(_ context.Context, cmd string, _ int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", totalSize)}, nil
		}
		if strings.Contains(cmd, "dd if=") {
			capturedDDCmd = cmd
			i := chunkIdx
			chunkIdx++
			return &types.ExecResponse{Output: chunkEncoded[i] + "\n"}, nil
		}
		if strings.Contains(cmd, "md5sum") {
			return &types.ExecResponse{Output: hash + "\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1, Output: "unexpected: " + cmd}, nil
	}

	localPath := filepath.Join(t.TempDir(), "chunked_encoder.bin")
	err := Pull(context.Background(), exec, "/remote/large.bin", localPath, PullOpts{
		Encoder:     customEncoder,
		Checksummer: testChecksummer,
		ChunkSize:   chunkSize,
	})

	require.NoError(t, err)
	assert.Contains(t, capturedDDCmd, "openssl enc -base64",
		"pullChunked must use encoder.Command; got: %s", capturedDDCmd)
	assert.NotContains(t, capturedDDCmd, "| base64\n",
		"must NOT use hardcoded '| base64'")
}

// TestPullSmallFile verifies a file ≤ 16 MB is transferred via a single base64 command.
func TestPullSmallFile(t *testing.T) {
	t.Parallel()

	content := []byte("Hello, World! This is test content for pull.")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := len(content)

	hash := hexMD5(content)

	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: fmt.Sprintf("  %d\n", size)}, nil
		}
		if strings.Contains(cmd, "base64") {
			return &types.ExecResponse{Output: encoded + "\n"}, nil
		}
		if strings.Contains(cmd, "md5sum") {
			return &types.ExecResponse{Output: hash + "\n"}, nil
		}
		return &types.ExecResponse{}, nil
	}

	localPath := filepath.Join(t.TempDir(), "output.txt")
	err := Pull(context.Background(), exec, "/remote/file.txt", localPath, PullOpts{
		Encoder:     testEncoder(),
		Checksummer: testChecksummer,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// TestPullLargeFileChunked verifies a file > 16 MB is split into dd chunks
// and reassembled correctly.
//
// To keep memory usage reasonable we use a small chunkSize (32 bytes) and a
// totalSize just above the 16 MB threshold so the chunked path is exercised
// without allocating hundreds of MB in test data.
func TestPullLargeFileChunked(t *testing.T) {
	t.Parallel()

	// Use 6 MB chunks and a 17 MB total so we get 3 chunks (2 full + 1 partial)
	// while staying clearly above the 16 MB smallFileThreshold.
	chunkSize := 6 * 1024 * 1024                       // 6 MB per chunk
	totalSize := int64(smallFileThreshold + 1024*1024) // 17 MB

	// Build predictable content.
	content := make([]byte, totalSize)
	for i := range content {
		content[i] = byte(i % 256)
	}

	// Pre-encode each expected chunk.
	numChunks := int((totalSize + int64(chunkSize) - 1) / int64(chunkSize))
	chunkEncoded := make([]string, numChunks)
	for i := 0; i < numChunks; i++ {
		offset := int64(i) * int64(chunkSize)
		end := offset + int64(chunkSize)
		if end > totalSize {
			end = totalSize
		}
		chunkEncoded[i] = base64.StdEncoding.EncodeToString(content[offset:end])
	}

	var chunkIdx int32 = -1

	hash := hexMD5(content)

	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", totalSize)}, nil
		}
		if strings.Contains(cmd, "dd if=") {
			i := int(atomic.AddInt32(&chunkIdx, 1))
			if i >= numChunks {
				return &types.ExecResponse{ExitCode: 1, Output: "too many dd calls"}, nil
			}
			return &types.ExecResponse{Output: chunkEncoded[i] + "\n"}, nil
		}
		if strings.Contains(cmd, "md5sum") {
			return &types.ExecResponse{Output: hash + "\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1, Output: "unexpected: " + cmd}, nil
	}

	localPath := filepath.Join(t.TempDir(), "large.bin")
	err := Pull(context.Background(), exec, "/remote/large.bin", localPath, PullOpts{
		Encoder:     testEncoder(),
		ChunkSize:   chunkSize,
		Checksummer: testChecksummer,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// TestPullEmptyFile verifies that wc returning "0" → ErrEmptyFile, no file created.
func TestPullEmptyFile(t *testing.T) {
	t.Parallel()

	exec := pullMockExec([]pullHandler{
		{substr: "wc -c", resp: &types.ExecResponse{Output: "0\n"}},
	})

	localPath := filepath.Join(t.TempDir(), "empty.txt")
	err := Pull(context.Background(), exec, "/remote/empty.txt", localPath, PullOpts{
		Encoder:     testEncoder(),
		Checksummer: testChecksummer,
	})

	require.ErrorIs(t, err, ErrEmptyFile)
	_, statErr := os.Stat(localPath)
	assert.True(t, os.IsNotExist(statErr), "local file must not be created")
}

// TestPullMissingFile verifies that a non-zero exit from wc returns an error.
func TestPullMissingFile(t *testing.T) {
	t.Parallel()

	exec := pullMockExec([]pullHandler{
		{substr: "wc -c", resp: &types.ExecResponse{ExitCode: 1, Output: "no such file"}},
	})

	localPath := filepath.Join(t.TempDir(), "missing.txt")
	err := Pull(context.Background(), exec, "/remote/missing.txt", localPath, PullOpts{
		Encoder:     testEncoder(),
		Checksummer: testChecksummer,
	})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrEmptyFile)
}

// TestPullChecksumMatch verifies that a matching checksum produces nil error.
func TestPullChecksumMatch(t *testing.T) {
	t.Parallel()

	content := []byte("checksum-match content")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := len(content)
	hash := hexMD5(content)

	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		switch {
		case strings.Contains(cmd, "wc -c"):
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", size)}, nil
		case strings.Contains(cmd, "base64"):
			return &types.ExecResponse{Output: encoded + "\n"}, nil
		case strings.Contains(cmd, "md5sum"):
			return &types.ExecResponse{Output: hash + "\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1}, nil
	}

	localPath := filepath.Join(t.TempDir(), "checksummed.txt")
	opts := PullOpts{
		Encoder: testEncoder(),
		Checksummer: &Checksummer{
			Name:           "md5sum",
			VerifyTemplate: "md5sum %s | awk '{print $1}'",
			HashAlgo:       "md5",
		},
	}
	err := Pull(context.Background(), exec, "/remote/checksummed.txt", localPath, opts)
	require.NoError(t, err)
}

// TestPullChecksumMismatch verifies that a wrong remote hash → ErrChecksumMismatch.
func TestPullChecksumMismatch(t *testing.T) {
	t.Parallel()

	content := []byte("mismatch content")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := len(content)

	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		switch {
		case strings.Contains(cmd, "wc -c"):
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", size)}, nil
		case strings.Contains(cmd, "base64"):
			return &types.ExecResponse{Output: encoded + "\n"}, nil
		case strings.Contains(cmd, "md5sum"):
			return &types.ExecResponse{Output: "deadbeefdeadbeefdeadbeefdeadbeef\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1}, nil
	}

	localPath := filepath.Join(t.TempDir(), "mismatch.txt")
	opts := PullOpts{
		Encoder: testEncoder(),
		Checksummer: &Checksummer{
			Name:           "md5sum",
			VerifyTemplate: "md5sum %s | awk '{print $1}'",
			HashAlgo:       "md5",
		},
	}
	err := Pull(context.Background(), exec, "/remote/mismatch.txt", localPath, opts)

	require.ErrorIs(t, err, ErrChecksumMismatch)
}

// TestPullTempFileCleanupOnError verifies that a failed transfer leaves no temp file.
func TestPullTempFileCleanupOnError(t *testing.T) {
	t.Parallel()

	xferErr := errors.New("transport failure")
	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: "100\n"}, nil
		}
		if strings.Contains(cmd, "base64") {
			return nil, xferErr
		}
		return &types.ExecResponse{ExitCode: 1}, nil
	}

	dir := t.TempDir()
	localPath := filepath.Join(dir, "cleanup.txt")
	err := Pull(context.Background(), exec, "/remote/file.txt", localPath, PullOpts{
		Encoder:     testEncoder(),
		Checksummer: testChecksummer,
	})

	require.Error(t, err)

	// No .a2s-pull-* temp files should remain.
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".a2s-pull-"),
			"temp file not cleaned up: %s", e.Name())
	}
}

// TestPullProgressCallback verifies OnProgress is called with correct (transferred, total) values.
func TestPullProgressCallback(t *testing.T) {
	t.Parallel()

	content := []byte("progress test content for pull function")
	encoded := base64.StdEncoding.EncodeToString(content)
	size := int64(len(content))

	hash := hexMD5(content)

	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		if strings.Contains(cmd, "wc -c") {
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", size)}, nil
		}
		if strings.Contains(cmd, "base64") {
			return &types.ExecResponse{Output: encoded + "\n"}, nil
		}
		if strings.Contains(cmd, "md5sum") {
			return &types.ExecResponse{Output: hash + "\n"}, nil
		}
		return &types.ExecResponse{ExitCode: 1}, nil
	}

	var progressCalls [][2]int64
	opts := PullOpts{
		Encoder:     testEncoder(),
		Checksummer: testChecksummer,
		OnProgress: func(transferred, total int64) {
			progressCalls = append(progressCalls, [2]int64{transferred, total})
		},
	}

	localPath := filepath.Join(t.TempDir(), "progress.txt")
	err := Pull(context.Background(), exec, "/remote/progress.txt", localPath, opts)
	require.NoError(t, err)

	require.Len(t, progressCalls, 1)
	assert.Equal(t, size, progressCalls[0][0]) // transferred == size
	assert.Equal(t, size, progressCalls[0][1]) // total == size
}

// TestPullContextCancellation verifies that a cancelled context propagates correctly
// and leaves no temp file behind.
func TestPullContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before any call

	exec := func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		return nil, ctx.Err()
	}

	dir := t.TempDir()
	localPath := filepath.Join(dir, "cancelled.txt")
	err := Pull(ctx, exec, "/remote/file.txt", localPath, PullOpts{
		Encoder:     testEncoder(),
		Checksummer: testChecksummer,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// No temp file left.
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".a2s-pull-"),
			"temp file not cleaned up: %s", e.Name())
	}
}
