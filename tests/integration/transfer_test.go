package integration

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/transfer"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── mockFS ───────────────────────────────────────────────────────────────────

// mockFS is an in-memory filesystem used by the integration handler. It
// interprets shell commands produced by the transfer package and simulates the
// remote target's filesystem without requiring a real shell.
type mockFS struct {
	mu    sync.Mutex
	files map[string][]byte
}

// regexes for command matching — compiled once at package init.
var (
	rePushWrite     = regexp.MustCompile(`^printf '%s' '([^']+)' \| base64 --decode > '(.+)'$`)
	rePushAppend    = regexp.MustCompile(`^printf '%s' '([^']+)' \| base64 --decode >> '(.+)'$`)
	reMD5Sum        = regexp.MustCompile(`^md5sum '(.+)' \| awk '\{print \$1\}'$`)
	reWC            = regexp.MustCompile(`^wc -c < '(.+)'$`)
	reBase64Read    = regexp.MustCompile(`^base64 < '(.+)'$`)
	reDD            = regexp.MustCompile(`^dd if='(.+)' bs=1 skip=(\d+) count=(\d+) 2>/dev/null \| base64$`)
	reDecoderProbe  = regexp.MustCompile(`^echo 'dGVzdA==' \| base64 --decode 2>/dev/null$`)
	reChecksumProbe = regexp.MustCompile(`^echo -n 'test' \| md5sum 2>/dev/null$`)
)

// handleCommand parses a shell command and performs the equivalent operation on
// the in-memory filesystem. Returns an ExecResponse compatible with the
// transfer package expectations.
func (fs *mockFS) handleCommand(cmd string) (*types.ExecResponse, error) {
	ok := func(out string) (*types.ExecResponse, error) {
		return &types.ExecResponse{Output: out, ExitCode: 0}, nil
	}

	// ── detection probes ──────────────────────────────────────────────────

	if reDecoderProbe.MatchString(cmd) {
		return ok("test")
	}

	if reChecksumProbe.MatchString(cmd) {
		h := md5.Sum([]byte("test"))
		return ok(fmt.Sprintf("%x  -\n", h))
	}

	// ── push write (>) ────────────────────────────────────────────────────

	if m := rePushWrite.FindStringSubmatch(cmd); m != nil {
		b64, path := m[1], m[2]
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return &types.ExecResponse{ExitCode: 1, Output: err.Error()}, nil
		}
		fs.mu.Lock()
		fs.files[path] = data
		fs.mu.Unlock()
		return ok("")
	}

	// ── push append (>>) ──────────────────────────────────────────────────

	if m := rePushAppend.FindStringSubmatch(cmd); m != nil {
		b64, path := m[1], m[2]
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return &types.ExecResponse{ExitCode: 1, Output: err.Error()}, nil
		}
		fs.mu.Lock()
		fs.files[path] = append(fs.files[path], data...)
		fs.mu.Unlock()
		return ok("")
	}

	// ── checksum: md5sum '/path' | awk '{print $1}' ───────────────────────

	if m := reMD5Sum.FindStringSubmatch(cmd); m != nil {
		path := m[1]
		fs.mu.Lock()
		content, exists := fs.files[path]
		fs.mu.Unlock()
		if !exists {
			return &types.ExecResponse{ExitCode: 1, Output: "No such file"}, nil
		}
		h := md5.Sum(content)
		return ok(fmt.Sprintf("%x", h))
	}

	// ── pull size: wc -c < '/path' ────────────────────────────────────────

	if m := reWC.FindStringSubmatch(cmd); m != nil {
		path := m[1]
		fs.mu.Lock()
		content, exists := fs.files[path]
		fs.mu.Unlock()
		if !exists {
			return &types.ExecResponse{ExitCode: 1, Output: "No such file"}, nil
		}
		return ok(fmt.Sprintf("%d\n", len(content)))
	}

	// ── pull small: base64 < '/path' ──────────────────────────────────────

	if m := reBase64Read.FindStringSubmatch(cmd); m != nil {
		path := m[1]
		fs.mu.Lock()
		content, exists := fs.files[path]
		fs.mu.Unlock()
		if !exists {
			return &types.ExecResponse{ExitCode: 1, Output: "No such file"}, nil
		}
		return ok(base64.StdEncoding.EncodeToString(content))
	}

	// ── pull chunked: dd if='/path' bs=1 skip=N count=M 2>/dev/null | base64

	if m := reDD.FindStringSubmatch(cmd); m != nil {
		path := m[1]
		skip, _ := strconv.ParseInt(m[2], 10, 64)
		count, _ := strconv.ParseInt(m[3], 10, 64)

		fs.mu.Lock()
		content, exists := fs.files[path]
		fs.mu.Unlock()
		if !exists {
			return &types.ExecResponse{ExitCode: 1, Output: "No such file"}, nil
		}

		end := skip + count
		if end > int64(len(content)) {
			end = int64(len(content))
		}
		chunk := content[skip:end]
		return ok(base64.StdEncoding.EncodeToString(chunk))
	}

	return &types.ExecResponse{ExitCode: 1, Output: fmt.Sprintf("mockFS: unrecognised command: %s", cmd)}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// newTransferServer starts a socket.Server backed by a fresh mockFS and returns
// the socket path and a reference to the mock filesystem for assertions.
func newTransferServer(t *testing.T) (sockPath string, fs *mockFS) {
	t.Helper()
	fs = &mockFS{files: make(map[string][]byte)}

	handler := func(ctx context.Context, req *types.Request) (any, error) {
		if req.Type != types.RunRequest {
			return nil, fmt.Errorf("unexpected request type: %s", req.Type)
		}
		return fs.handleCommand(req.Command)
	}

	sockPath = startServer(t, handler)
	return sockPath, fs
}

// makeExec wraps client.Run into a transfer.ExecFunc for the given socket path.
func makeExec(sockPath string) transfer.ExecFunc {
	return func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		return client.Run(ctx, sockPath, cmd, timeout)
	}
}

// randomBytes generates a deterministic byte slice of length n.
func randomBytes(n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(i % 251) // 251 is prime → varied pattern
	}
	return buf
}

// ─── T1: Push + Pull round-trip ───────────────────────────────────────────────

// TestTransfer_PushPullRoundTrip verifies that Push stores content in the mock
// filesystem and Pull retrieves the exact same bytes back to a local file.
func TestTransfer_PushPullRoundTrip(t *testing.T) {
	t.Parallel()

	sockPath, fs := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	// Detect decoder and checksummer through the real socket.
	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, dec)

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	// Write local source file.
	want := []byte("Hello from integration test!")
	localIn := filepath.Join(t.TempDir(), "input.txt")
	require.NoError(t, os.WriteFile(localIn, want, 0o600))

	// Push to mock remote.
	remotePath := "/remote/test.txt"
	err = transfer.Push(ctx, exec, localIn, remotePath, transfer.PushOpts{
		Decoder:     dec,
		Checksummer: chk,
	})
	require.NoError(t, err)

	// Verify in-memory store.
	assert.Equal(t, want, fs.files[remotePath])

	// Pull back to a local file.
	localOut := filepath.Join(t.TempDir(), "output.txt")
	err = transfer.Pull(ctx, exec, remotePath, localOut, transfer.PullOpts{
		Checksummer: chk,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(localOut)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ─── T2: Push + Pull with spaces in path ─────────────────────────────────────

// TestTransfer_PathWithSpaces verifies that shell-quoting is applied correctly
// when the remote path contains spaces.
func TestTransfer_PathWithSpaces(t *testing.T) {
	t.Parallel()

	sockPath, fs := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.NoError(t, err)

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	want := []byte("spaced content")
	localIn := filepath.Join(t.TempDir(), "in.txt")
	require.NoError(t, os.WriteFile(localIn, want, 0o600))

	remotePath := "/remote/dir with spaces/file.txt"
	err = transfer.Push(ctx, exec, localIn, remotePath, transfer.PushOpts{Decoder: dec, Checksummer: chk})
	require.NoError(t, err)
	assert.Equal(t, want, fs.files[remotePath])

	localOut := filepath.Join(t.TempDir(), "out.txt")
	err = transfer.Pull(ctx, exec, remotePath, localOut, transfer.PullOpts{Checksummer: chk})
	require.NoError(t, err)

	got, err := os.ReadFile(localOut)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ─── T3: Push multi-chunk ─────────────────────────────────────────────────────

// TestTransfer_PushMultiChunk verifies that a file larger than DefaultChunkSize
// is split into multiple chunks that are correctly reassembled in the mock FS.
func TestTransfer_PushMultiChunk(t *testing.T) {
	t.Parallel()

	sockPath, fs := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.NoError(t, err)

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	// 2.5 chunks worth of data.
	want := randomBytes(transfer.DefaultChunkSize*2 + transfer.DefaultChunkSize/2)
	localIn := filepath.Join(t.TempDir(), "big.bin")
	require.NoError(t, os.WriteFile(localIn, want, 0o600))

	remotePath := "/remote/big.bin"
	err = transfer.Push(ctx, exec, localIn, remotePath, transfer.PushOpts{Decoder: dec, Checksummer: chk})
	require.NoError(t, err)

	// The mock FS must have the full, reassembled content.
	require.True(t, bytes.Equal(want, fs.files[remotePath]),
		"stored content differs: got %d bytes, want %d bytes",
		len(fs.files[remotePath]), len(want))
}

// ─── T4: Push with checksum verification ─────────────────────────────────────

// TestTransfer_PushWithChecksum verifies that when a Checksummer is provided
// Push calls the checksum command and the transfer succeeds when hashes match.
func TestTransfer_PushWithChecksum(t *testing.T) {
	t.Parallel()

	sockPath, _ := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.NoError(t, err)

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk, "mockFS must respond to md5sum probe")

	want := []byte("checksum me please")
	localIn := filepath.Join(t.TempDir(), "chk.txt")
	require.NoError(t, os.WriteFile(localIn, want, 0o600))

	err = transfer.Push(ctx, exec, localIn, "/remote/chk.txt", transfer.PushOpts{
		Decoder:     dec,
		Checksummer: chk,
	})
	require.NoError(t, err)
}

// ─── T5: Pull small file ──────────────────────────────────────────────────────

// TestTransfer_PullSmallFile pre-populates the mock FS and verifies that Pull
// retrieves the correct content using the single-base64 path (≤ 16 MB).
func TestTransfer_PullSmallFile(t *testing.T) {
	t.Parallel()

	sockPath, fs := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	want := []byte("small file content for pull test")
	fs.mu.Lock()
	fs.files["/remote/small.txt"] = want
	fs.mu.Unlock()

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	localOut := filepath.Join(t.TempDir(), "out.txt")
	err = transfer.Pull(ctx, exec, "/remote/small.txt", localOut, transfer.PullOpts{
		Checksummer: chk,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(localOut)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ─── T6: Decoder detection through socket ────────────────────────────────────

// TestTransfer_DetectDecoderIntegration verifies that DetectDecoder succeeds
// through a real Unix socket with a handler that responds to the base64 probe.
func TestTransfer_DetectDecoderIntegration(t *testing.T) {
	t.Parallel()

	sockPath, _ := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, dec)
	assert.Equal(t, "base64", dec.Name)
	assert.Equal(t, "base64 --decode", dec.Command)
}

// ─── T7: Pull + Push with checksum round-trip ────────────────────────────────

// TestTransfer_PullWithChecksum pre-populates the mock FS and verifies that a
// Pull with a Checksummer succeeds when the remote and local hashes agree.
func TestTransfer_PullWithChecksum(t *testing.T) {
	t.Parallel()

	sockPath, fs := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	want := []byte("pull with checksum content")
	fs.mu.Lock()
	fs.files["/remote/chk_pull.txt"] = want
	fs.mu.Unlock()

	localOut := filepath.Join(t.TempDir(), "out.txt")
	err = transfer.Pull(ctx, exec, "/remote/chk_pull.txt", localOut, transfer.PullOpts{
		Checksummer: chk,
	})
	require.NoError(t, err)

	got, err := os.ReadFile(localOut)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// ─── T8: Pull empty file returns ErrEmptyFile ────────────────────────────────

// TestTransfer_PullEmptyFile verifies that Pull returns ErrEmptyFile when the
// remote file exists but is empty (wc -c returns 0).
func TestTransfer_PullEmptyFile(t *testing.T) {
	t.Parallel()

	sockPath, fs := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	fs.mu.Lock()
	fs.files["/remote/empty.txt"] = []byte{}
	fs.mu.Unlock()

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	localOut := filepath.Join(t.TempDir(), "out.txt")
	err = transfer.Pull(ctx, exec, "/remote/empty.txt", localOut, transfer.PullOpts{
		Checksummer: chk,
	})
	require.ErrorIs(t, err, transfer.ErrEmptyFile)

	// localOut must NOT exist after a failed pull.
	_, statErr := os.Stat(localOut)
	assert.True(t, os.IsNotExist(statErr), "output file must not be created on ErrEmptyFile")
}

// ─── T9: DetectDecoder returns ErrNoDecoder when no probe matches ─────────────

// TestTransfer_DetectDecoderNoneAvailable verifies that DetectDecoder returns
// ErrNoDecoder when the handler returns exit 1 for all probes.
func TestTransfer_DetectDecoderNoneAvailable(t *testing.T) {
	t.Parallel()

	// Handler that always fails every RunRequest.
	failHandler := func(ctx context.Context, req *types.Request) (any, error) {
		if req.Type != types.RunRequest {
			return nil, fmt.Errorf("unexpected type: %s", req.Type)
		}
		return &types.ExecResponse{ExitCode: 1, Output: "command not found"}, nil
	}

	sockPath := startServer(t, failHandler)
	ctx := context.Background()
	exec := makeExec(sockPath)

	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.ErrorIs(t, err, transfer.ErrNoDecoder)
	assert.Nil(t, dec)
}

// ─── T10: Push to non-existent path ──────────────────────────────────────────

// TestTransfer_PushLocalFileNotFound verifies that Push returns an error when
// the local source file does not exist.
func TestTransfer_PushLocalFileNotFound(t *testing.T) {
	t.Parallel()

	sockPath, _ := newTransferServer(t)
	ctx := context.Background()
	exec := makeExec(sockPath)

	dec, err := transfer.DetectDecoder(ctx, exec, 5)
	require.NoError(t, err)

	chk, err := transfer.DetectChecksum(ctx, exec, 5)
	require.NoError(t, err)
	require.NotNil(t, chk)

	err = transfer.Push(ctx, exec, "/definitely/does/not/exist.txt", "/remote/out.txt", transfer.PushOpts{
		Decoder:     dec,
		Checksummer: chk,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open")
}

// ─── T11: unquoteShellPath helper test for reMD5Sum regex ────────────────────

// TestMockFS_HandleCommand_UnknownCommand verifies that unrecognised commands
// return exit code 1 (not a Go error) so the transfer layer can handle them.
func TestMockFS_HandleCommand_UnknownCommand(t *testing.T) {
	t.Parallel()

	fs := &mockFS{files: make(map[string][]byte)}
	resp, err := fs.handleCommand("some-unknown-command --flag")
	require.NoError(t, err)
	assert.Equal(t, 1, resp.ExitCode)
	assert.Contains(t, resp.Output, "unrecognised")
}

// ─── T12: checksum mismatch on pull ──────────────────────────────────────────

// TestTransfer_PullChecksumMismatch verifies that if the remote md5sum differs
// from the locally computed hash Pull returns ErrChecksumMismatch.
func TestTransfer_PullChecksumMismatch(t *testing.T) {
	t.Parallel()

	// Handler that serves the file correctly but returns a wrong checksum.
	content := []byte("original content")
	badChecksum := strings.Repeat("0", 32)

	handler := func(ctx context.Context, req *types.Request) (any, error) {
		if req.Type != types.RunRequest {
			return nil, fmt.Errorf("unexpected type")
		}
		cmd := req.Command
		switch {
		case reWC.MatchString(cmd):
			return &types.ExecResponse{Output: fmt.Sprintf("%d\n", len(content)), ExitCode: 0}, nil
		case reBase64Read.MatchString(cmd):
			return &types.ExecResponse{Output: base64.StdEncoding.EncodeToString(content), ExitCode: 0}, nil
		case reMD5Sum.MatchString(cmd):
			return &types.ExecResponse{Output: badChecksum, ExitCode: 0}, nil
		default:
			return &types.ExecResponse{ExitCode: 1, Output: "unknown"}, nil
		}
	}

	sockPath := startServer(t, handler)
	ctx := context.Background()
	exec := makeExec(sockPath)

	// We need a Checksummer; use md5sum directly since we know it's mocked.
	chk := &transfer.Checksummer{Name: "md5sum", Command: "md5sum"}

	localOut := filepath.Join(t.TempDir(), "out.txt")
	err := transfer.Pull(ctx, exec, "/remote/file.txt", localOut, transfer.PullOpts{
		Checksummer: chk,
	})
	require.ErrorIs(t, err, transfer.ErrChecksumMismatch)
}
