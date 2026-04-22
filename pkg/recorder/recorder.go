// Package recorder writes programmatic exec outcomes to a JSONL file.
// Each entry is a single JSON object followed by a newline. The file is
// opened for append so multiple sessions accumulate in the same log.
package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Entry holds the outcome of a single programmatic exec.
type Entry struct {
	Timestamp  string `json:"timestamp"`
	Command    string `json:"command"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// Recorder appends exec entries to a JSONL file. All methods are safe for
// concurrent use.
type Recorder struct {
	mu   sync.Mutex
	file *os.File
}

// New opens path for append-only writing (O_CREATE|O_WRONLY|O_APPEND, mode
// 0600) and returns a Recorder ready to accept Log calls.
func New(path string) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("recorder: open %s: %w", path, err)
	}
	return &Recorder{file: f}, nil
}

// Log marshals entry to JSON and writes it as a single line followed by '\n'.
// The write is mutex-protected so concurrent callers never interleave bytes.
func (r *Recorder) Log(entry Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("recorder: marshal entry: %w", err)
	}
	data = append(data, '\n')

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.file.Write(data); err != nil {
		return fmt.Errorf("recorder: write entry: %w", err)
	}
	return nil
}

// Close closes the underlying file. Calling Close more than once returns
// whatever the OS returns for a double-close (non-nil error on the second
// call), which callers may safely ignore.
func (r *Recorder) Close() error {
	if err := r.file.Close(); err != nil {
		return fmt.Errorf("recorder: close: %w", err)
	}
	return nil
}
