package transfer

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execCall records a single call to the mock ExecFunc.
type execCall struct {
	command string
}

// mockExecIndexed returns an ExecFunc that records all calls and serves
// responses by call index. Unmatched indices return exit-0 success.
func mockExecIndexed(calls *[]execCall, responses map[int]*types.ExecResponse, errors map[int]error) ExecFunc {
	var mu sync.Mutex
	idx := 0
	return func(ctx context.Context, command string, timeout int) (*types.ExecResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		*calls = append(*calls, execCall{command: command})
		i := idx
		idx++
		if errors != nil {
			if err, ok := errors[i]; ok {
				return nil, err
			}
		}
		if responses != nil {
			if resp, ok := responses[i]; ok {
				return resp, nil
			}
		}
		return &types.ExecResponse{ExitCode: 0}, nil
	}
}

var testChecksummer = &Checksummer{Name: "md5sum", Command: "md5sum"}

func decoder() *Decoder {
	return &Decoder{Name: "base64", Command: "base64 --decode"}
}

func checksummer() *Checksummer {
	return &Checksummer{Name: "md5sum"}
}

// writeTempFile creates a temp file with exactly n bytes and returns its path.
func writeTempFile(t *testing.T, n int) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "push_test_*")
	require.NoError(t, err)
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i % 256)
	}
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestPush_NilDecoder(t *testing.T) {
	t.Parallel()

	var calls []execCall
	exec := mockExecIndexed(&calls, nil, nil)

	err := Push(context.Background(), exec, "/dev/null", "/remote/path", PushOpts{
		Decoder: nil,
	})

	assert.ErrorIs(t, err, ErrNoDecoder)
	assert.Empty(t, calls, "no exec calls expected when decoder is nil")
}

func TestPush_SingleChunk(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 100)
	remotePath := "/tmp/output.bin"

	var calls []execCall
	var progressCalls []int64

	// call 0 = chunk, call 1 = checksum verification (md5sum)
	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	expectedMD5 := computeMD5Helper(t, data)
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: expectedMD5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err = Push(context.Background(), exec, localPath, remotePath, PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
		OnProgress: func(transferred, total int64) {
			progressCalls = append(progressCalls, transferred)
		},
	})

	require.NoError(t, err)
	// 1 chunk exec + 1 checksum
	require.Len(t, calls, 2, "expected 2 exec calls: 1 chunk + 1 checksum")
	assert.Contains(t, calls[0].command, "> '/tmp/output.bin'", "first chunk must use > redirect")
	assert.Len(t, progressCalls, 1, "OnProgress called once")
	assert.Equal(t, int64(100), progressCalls[0])
}

func TestPush_MultiChunk(t *testing.T) {
	t.Parallel()

	// Write a file larger than DefaultChunkSize to force multiple chunks
	fileSize := DefaultChunkSize*2 + 512
	localPath := writeTempFile(t, fileSize)
	remotePath := "/tmp/multi.bin"

	var calls []execCall
	var progressVals []int64

	// calls 0,1,2 = chunks; call 3 = checksum; compute expected md5
	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	expectedMD5 := computeMD5Helper(t, data)
	responses := map[int]*types.ExecResponse{
		3: {ExitCode: 0, Output: expectedMD5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err = Push(context.Background(), exec, localPath, remotePath, PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
		OnProgress: func(transferred, total int64) {
			progressVals = append(progressVals, transferred)
		},
	})

	require.NoError(t, err)

	// Expect 3 chunk calls + 1 checksum
	require.Len(t, calls, 4, "expected 4 exec calls: 3 chunks + 1 checksum")
	assert.Contains(t, calls[0].command, "> '/tmp/multi.bin'", "chunk 0 must use >")
	assert.Contains(t, calls[1].command, ">> '/tmp/multi.bin'", "chunk 1 must use >>")
	assert.Contains(t, calls[2].command, ">> '/tmp/multi.bin'", "chunk 2 must use >>")
	assert.Len(t, progressVals, 3, "OnProgress called once per chunk")
}

func TestPush_ShellQuotingInCommands(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 50)
	remotePath := "/tmp/path with spaces/file.bin"

	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	expectedMD5 := computeMD5Helper(t, data)

	var calls []execCall
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: expectedMD5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err = Push(context.Background(), exec, localPath, remotePath, PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
	})

	require.NoError(t, err)
	require.NotEmpty(t, calls)
	// shellQuote wraps in single quotes
	assert.Contains(t, calls[0].command, "'/tmp/path with spaces/file.bin'")
}

func TestPush_ChunkFailureExitCode(t *testing.T) {
	t.Parallel()

	// 3 chunks worth of data
	fileSize := DefaultChunkSize*2 + 512
	localPath := writeTempFile(t, fileSize)

	var calls []execCall
	// chunk index 1 (0-based, second chunk) returns exit 1
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 1, Output: "write error"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err := Push(context.Background(), exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2/3", "error should mention human-readable chunk index")
}

func TestPush_TransportError(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 100)

	var calls []execCall
	transportErr := errors.New("connection reset")
	errs := map[int]error{0: transportErr}
	exec := mockExecIndexed(&calls, nil, errs)

	err := Push(context.Background(), exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1/1")
}

func TestPush_ChecksumPass(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 100)

	// Compute the expected MD5 of the file so our mock can return it
	data, err := os.ReadFile(localPath)
	require.NoError(t, err)

	import_md5 := computeMD5Helper(t, data)

	var calls []execCall
	// call index 0 = chunk, call index 1 = checksum cmd
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: import_md5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err = Push(context.Background(), exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: checksummer(),
	})

	require.NoError(t, err)
	assert.Len(t, calls, 2, "1 chunk + 1 checksum call")
}

func TestPush_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 100)

	var calls []execCall
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: "deadbeefdeadbeefdeadbeefdeadbeef\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err := Push(context.Background(), exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: checksummer(),
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChecksumMismatch)
}

func TestPush_NilChecksummer(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 100)

	var calls []execCall
	exec := mockExecIndexed(&calls, nil, nil)

	err := Push(context.Background(), exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: nil,
	})

	require.ErrorIs(t, err, ErrNoChecksummer)
	assert.Empty(t, calls, "no exec calls expected when checksummer is nil")
}

func TestPush_NilOnProgress(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 100)

	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	expectedMD5 := computeMD5Helper(t, data)

	var calls []execCall
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: expectedMD5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	// Must not panic
	err = Push(context.Background(), exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
		OnProgress:  nil,
	})

	require.NoError(t, err)
}

func TestPush_ContextCancellation(t *testing.T) {
	t.Parallel()

	// File large enough for multiple chunks so cancellation mid-way is possible
	fileSize := DefaultChunkSize*5 + 100
	localPath := writeTempFile(t, fileSize)

	ctx, cancel := context.WithCancel(context.Background())

	var calls []execCall
	callCount := 0
	exec := func(c context.Context, command string, timeout int) (*types.ExecResponse, error) {
		calls = append(calls, execCall{command: command})
		callCount++
		if callCount == 2 {
			cancel() // cancel after second chunk
		}
		return &types.ExecResponse{ExitCode: 0}, nil
	}

	err := Push(ctx, exec, localPath, "/remote/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPush_Base64EncodingInCommand(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 10)
	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	expectedEncoded := base64.StdEncoding.EncodeToString(data)
	expectedMD5 := computeMD5Helper(t, data)

	var calls []execCall
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: expectedMD5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err = Push(context.Background(), exec, localPath, "/out", PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
	})

	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Contains(t, calls[0].command, expectedEncoded, "command must contain base64-encoded data")
	assert.Contains(t, calls[0].command, "printf '%s'", "must use printf not echo")
	assert.Contains(t, calls[0].command, decoder().Command, "must pipe through decoder command")
}

// computeMD5Helper computes the hex MD5 of data for test assertions.
func computeMD5Helper(t *testing.T, data []byte) string {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "md5input")
	require.NoError(t, os.WriteFile(tmpFile, data, 0o600))
	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()
	hash, err := computeMD5(f)
	require.NoError(t, err)
	return hash
}

// TestPush_CommandStructure verifies the exact printf | decoder > path form.
func TestPush_CommandStructure(t *testing.T) {
	t.Parallel()

	localPath := writeTempFile(t, 20)
	remotePath := "/tmp/dest"

	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	expectedMD5 := computeMD5Helper(t, data)

	var calls []execCall
	responses := map[int]*types.ExecResponse{
		1: {ExitCode: 0, Output: expectedMD5 + "\n"},
	}
	exec := mockExecIndexed(&calls, responses, nil)

	err = Push(context.Background(), exec, localPath, remotePath, PushOpts{
		Decoder:     decoder(),
		Checksummer: testChecksummer,
	})

	require.NoError(t, err)
	require.Len(t, calls, 2)

	cmd := calls[0].command
	// Must have printf '%s' '<b64>' | base64 --decode > '/tmp/dest'
	assert.True(t, strings.HasPrefix(cmd, "printf '%s' '"), "must start with printf '%s' '")
	assert.Contains(t, cmd, "| base64 --decode >")
}
