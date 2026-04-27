package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Task 5.3: E2E tests for --stdin flag ─────────────────────────────────────

// TestRunStdin_MissingFileExitsNonZero verifies that `run --stdin /nonexistent bash`
// exits non-zero and includes the file path in the error output.
// A fake socket path is provided so resolveSocket doesn't fail first.
func TestRunStdin_MissingFileExitsNonZero(t *testing.T) {
	path := "/nonexistent/stdin_script_enoent.sh"
	// Use --socket so resolveSocket doesn't auto-discover (which would fail first
	// with a different "no sessions" error before we reach file reading).
	output, code := runBinary(t, "--socket", "/tmp/nonexistent_a2s_test.sock",
		"run", "--stdin", path, "bash")

	if code == 0 {
		t.Errorf("expected non-zero exit code for missing file, got 0")
	}
	if !contains(output, "stdin_script_enoent.sh") {
		t.Errorf("expected file path in error output, got: %q", output)
	}
}

// TestRunStdin_ShortFlagAccepted verifies that `-i` is accepted as the
// shorthand for `--stdin` (non-existent file errors in a recognisable way,
// rather than "unknown flag").
func TestRunStdin_ShortFlagAccepted(t *testing.T) {
	output, code := runBinary(t, "--socket", "/tmp/nonexistent_a2s_test.sock",
		"run", "-i", "/nonexistent_file_for_test_shorthand.sh", "bash")

	if code == 0 {
		t.Errorf("expected non-zero exit code for missing file, got 0")
	}
	// The error must mention the file, not "unknown shorthand flag"
	if contains(output, "unknown shorthand flag") {
		t.Errorf("-i shorthand not recognised: %q", output)
	}
}

// TestRunStdin_WithFile verifies that `run --stdin <file>` reads a valid file
// without crashing on flag parsing. The command will fail to connect (no server),
// but the failure must be a connection error — not a file-read error.
func TestRunStdin_WithFile(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("id\nwhoami\n"), 0o644); err != nil {
		t.Fatalf("write temp script: %v", err)
	}

	// Use a nonexistent socket so the error is "connection refused", not a file error.
	output, code := runBinary(t, "--socket", "/tmp/nonexistent_a2s_test.sock",
		"run", "--stdin", scriptPath)

	if code == 0 {
		t.Errorf("expected non-zero exit code (no server running), got 0")
	}
	// The error must NOT mention the script file (file was read successfully).
	if contains(output, "read file") || contains(output, scriptPath) {
		t.Errorf("expected connection error, got file error: %q", output)
	}
}

// TestRun_NoArgsNoStdinExitsNonZero verifies that `run` with no command args
// and no --stdin flag exits non-zero with a descriptive error.
func TestRun_NoArgsNoStdinExitsNonZero(t *testing.T) {
	output, code := runBinary(t, "run")
	if code == 0 {
		t.Errorf("expected non-zero exit code for run with no args, got 0")
	}
	_ = output // error message is printed, but we don't assert its exact form
}

// TestRun_NoArgsWithStdinDefaultsBash verifies that `run --stdin <file>` with
// no explicit command defaults to "bash" (the file read error does NOT say
// "requires at least 1 arg").
func TestRun_NoArgsWithStdinDefaultsBash(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("echo hello\n"), 0o644); err != nil {
		t.Fatalf("write temp script: %v", err)
	}

	// With no server, this should fail on connection, not arg validation.
	output, code := runBinary(t, "--socket", "/tmp/nonexistent_a2s_test.sock",
		"run", "--stdin", scriptPath)

	if code == 0 {
		t.Errorf("expected non-zero exit code (no server), got 0")
	}
	if contains(output, "requires at least 1 arg") {
		t.Errorf("--stdin with no args should default to bash, not fail arg validation: %q", output)
	}
}
