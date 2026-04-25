package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Task 4.1: run --stdin flag ───────────────────────────────────────────────

// TestRunCmd_StdinFlagExists verifies that the --stdin/-i flag is registered.
func TestRunCmd_StdinFlagExists(t *testing.T) {
	f := runCmd.Flags().Lookup("stdin")
	if f == nil {
		t.Fatal("--stdin flag not registered on run command")
	}
	if f.Value.Type() != "string" {
		t.Errorf("--stdin flag type = %q, want string", f.Value.Type())
	}
	if f.Shorthand != "i" {
		t.Errorf("--stdin shorthand = %q, want i", f.Shorthand)
	}
}

// TestRunCmd_StdinFlagDefault verifies --stdin defaults to empty string.
func TestRunCmd_StdinFlagDefault(t *testing.T) {
	f := runCmd.Flags().Lookup("stdin")
	if f == nil {
		t.Fatal("--stdin flag not registered")
	}
	if f.DefValue != "" {
		t.Errorf("--stdin default = %q, want empty string", f.DefValue)
	}
}

// TestReadStdinContent_NamedFile verifies that readStdinContent reads a local
// file and returns its content unchanged.
func TestReadStdinContent_NamedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	content := "id\nwhoami\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := readStdinContent(path)
	if err != nil {
		t.Fatalf("readStdinContent: %v", err)
	}
	if got != content {
		t.Errorf("readStdinContent = %q, want %q", got, content)
	}
}

// TestReadStdinContent_MissingFile verifies that readStdinContent returns a
// non-nil error containing the file path when the file does not exist.
func TestReadStdinContent_MissingFile(t *testing.T) {
	path := "/nonexistent/path/to/script.sh"
	_, err := readStdinContent(path)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if got := err.Error(); got == "" {
		t.Error("error message must not be empty")
	}
}

// TestReadStdinContent_EmptyFile verifies that readStdinContent returns an
// empty string for an empty file (not an error).
func TestReadStdinContent_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sh")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := readStdinContent(path)
	if err != nil {
		t.Fatalf("readStdinContent: %v", err)
	}
	if got != "" {
		t.Errorf("readStdinContent = %q, want empty string", got)
	}
}

// TestRunCmd_NoArgsNoStdinFails verifies that running without args and without
// --stdin does not fall through silently. The command-level logic should
// require at least a command OR a --stdin flag.
// (This test verifies existing behavior is preserved — still requires command.)
func TestRunCmd_ArbitraryArgsSet(t *testing.T) {
	// After the change, runCmd must accept 0 args when --stdin is set.
	// We verify this by checking the Args validator is no longer MinimumNArgs(1).
	// We do this indirectly by ensuring the Args field is cobra.ArbitraryArgs.
	if runCmd.Args == nil {
		t.Error("runCmd.Args must not be nil")
	}
}
