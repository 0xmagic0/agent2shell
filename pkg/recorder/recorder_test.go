package recorder_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.jsonl")

	r, err := recorder.New(path)
	require.NoError(t, err)
	require.NotNil(t, r)
	defer r.Close() //nolint:errcheck

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestNew_ErrorOnInvalidPath(t *testing.T) {
	_, err := recorder.New("/nonexistent/dir/test.jsonl")
	require.Error(t, err)
}

func TestLog_WritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.jsonl")

	r, err := recorder.New(path)
	require.NoError(t, err)

	entries := []recorder.Entry{
		{
			Timestamp:  "2026-04-22T10:00:00Z",
			Command:    "id",
			Output:     "uid=0(root)\n",
			ExitCode:   0,
			DurationMS: 42,
		},
		{
			Timestamp:  "2026-04-22T10:00:01Z",
			Command:    "whoami",
			Output:     "root\n",
			ExitCode:   0,
			DurationMS: 10,
			Error:      "",
		},
	}

	for _, e := range entries {
		require.NoError(t, r.Log(e))
	}
	require.NoError(t, r.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	require.Len(t, lines, 2)

	for i, line := range lines {
		var got recorder.Entry
		require.NoError(t, json.Unmarshal([]byte(line), &got), "line %d is not valid JSON", i)
		assert.Equal(t, entries[i].Command, got.Command)
		assert.Equal(t, entries[i].Output, got.Output)
		assert.Equal(t, entries[i].ExitCode, got.ExitCode)
		assert.Equal(t, entries[i].DurationMS, got.DurationMS)
	}
}

func TestLog_ErrorFieldOmittedWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.jsonl")

	r, err := recorder.New(path)
	require.NoError(t, err)
	defer r.Close() //nolint:errcheck

	require.NoError(t, r.Log(recorder.Entry{
		Timestamp: "2026-04-22T10:00:00Z",
		Command:   "ls",
		Output:    "file.txt\n",
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// error field must be absent when empty (omitempty)
	assert.NotContains(t, string(data), `"error"`)
}

func TestLog_ErrorFieldPresentWhenSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.jsonl")

	r, err := recorder.New(path)
	require.NoError(t, err)
	defer r.Close() //nolint:errcheck

	require.NoError(t, r.Log(recorder.Entry{
		Timestamp: "2026-04-22T10:00:00Z",
		Command:   "bad",
		Error:     "exec timeout",
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"error":"exec timeout"`)
}

func TestLog_ConcurrentSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.jsonl")

	r, err := recorder.New(path)
	require.NoError(t, err)
	defer r.Close() //nolint:errcheck

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			logErr := r.Log(recorder.Entry{
				Timestamp: "2026-04-22T10:00:00Z",
				Command:   "echo",
				Output:    strings.Repeat("x", n),
				ExitCode:  n,
			})
			assert.NoError(t, logErr)
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	assert.Len(t, lines, goroutines)

	for i, line := range lines {
		var e recorder.Entry
		assert.NoError(t, json.Unmarshal([]byte(line), &e), "line %d invalid JSON", i)
	}
}

func TestClose_IdempotentSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.jsonl")

	r, err := recorder.New(path)
	require.NoError(t, err)

	require.NoError(t, r.Close())
	// Second close should not panic; error is acceptable (already closed)
	_ = r.Close()
}

func TestNew_AppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "append.jsonl")

	// First session.
	r1, err := recorder.New(path)
	require.NoError(t, err)
	require.NoError(t, r1.Log(recorder.Entry{Timestamp: "t1", Command: "first"}))
	require.NoError(t, r1.Close())

	// Second session — must append, not truncate.
	r2, err := recorder.New(path)
	require.NoError(t, err)
	require.NoError(t, r2.Log(recorder.Entry{Timestamp: "t2", Command: "second"}))
	require.NoError(t, r2.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	assert.Len(t, lines, 2)
}
