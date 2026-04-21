// Package e2e contains end-to-end tests for the agent2shell binary.
// Tests build the binary once in TestMain and run it as a subprocess.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// binaryPath holds the path to the compiled agent2shell binary.
var binaryPath string

// TestMain builds the binary once before running all tests, then cleans up.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent2shell-e2e-*")
	if err != nil {
		panic("failed to create temp dir: " + err.Error())
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "agent2shell")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	// Build the binary without ldflags for most tests.
	build := exec.Command("go", "build", "-o", bin, "./cmd/agent2shell")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		panic("build failed: " + err.Error() + "\n" + string(out))
	}

	binaryPath = bin
	os.Exit(m.Run())
}

// repoRoot returns the absolute path to the repository root.
// It walks up from the directory of this test file.
func repoRoot() string {
	// __file__ is not available at runtime; use os.Getwd and walk up.
	// Since `go test ./tests/e2e/...` is invoked from the repo root via make,
	// we rely on the module root by resolving the module cache.
	// Safest approach: use runtime.Caller to get this file's path.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot determine source file path")
	}
	// filename is .../tests/e2e/cli_test.go → go up two levels.
	return filepath.Join(filepath.Dir(filename), "..", "..")
}

// runBinary executes the agent2shell binary with the given arguments and
// returns the combined stdout+stderr output and the exit code.
func runBinary(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	out, err := cmd.CombinedOutput()
	output = string(out)
	if err == nil {
		return output, 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return output, exitErr.ExitCode()
	}
	t.Fatalf("unexpected error running binary: %v", err)
	return "", -1
}

// TestS51_NoArgsExitsZero verifies that the binary exits 0 when called with
// no arguments (displaying help is acceptable).
func TestS51_NoArgsExitsZero(t *testing.T) {
	_, code := runBinary(t)
	if code != 0 {
		t.Errorf("expected exit code 0 with no args, got %d", code)
	}
}

// TestS52_UnknownCommandExits127 verifies that an unknown subcommand causes
// the binary to exit with code 127.
func TestS52_UnknownCommandExits127(t *testing.T) {
	_, code := runBinary(t, "unknowncmd")
	if code != 127 {
		t.Errorf("expected exit code 127 for unknown command, got %d", code)
	}
}

// TestS53_SocketFlagParsesWithoutError verifies that --socket / -s is
// recognised as a valid flag.
func TestS53_SocketFlagParsesWithoutError(t *testing.T) {
	_, code := runBinary(t, "--socket", "/tmp/test.sock")
	if code != 0 {
		t.Errorf("expected exit code 0 with --socket flag, got %d", code)
	}
}

// TestS53_SocketShortFlagParsesWithoutError verifies the short form -s.
func TestS53_SocketShortFlagParsesWithoutError(t *testing.T) {
	_, code := runBinary(t, "-s", "/tmp/test.sock")
	if code != 0 {
		t.Errorf("expected exit code 0 with -s flag, got %d", code)
	}
}

// TestS54_NoColorFlagParsesWithoutError verifies that --no-color is recognised.
func TestS54_NoColorFlagParsesWithoutError(t *testing.T) {
	_, code := runBinary(t, "--no-color")
	if code != 0 {
		t.Errorf("expected exit code 0 with --no-color flag, got %d", code)
	}
}

// TestS55_VersionVarsInjectableViaLdflags verifies that the binary can be
// built with version ldflags and that --version outputs the injected value.
func TestS55_VersionVarsInjectableViaLdflags(t *testing.T) {
	dir, err := os.MkdirTemp("", "agent2shell-ver-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "agent2shell")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	const (
		wantVersion   = "v0.1.0"
		wantCommit    = "abc1234"
		wantBuildDate = "2026-04-21"
	)

	ldflags := "-X main.Version=" + wantVersion +
		" -X main.Commit=" + wantCommit +
		" -X main.BuildDate=" + wantBuildDate

	build := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, "./cmd/agent2shell")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build with ldflags failed: %v\n%s", err, out)
	}

	// Run with --version; cobra prints "agent2shell version <Version>".
	cmd := exec.Command(bin, "--version")
	out, err := cmd.CombinedOutput()
	output := string(out)

	// --version exits 0.
	if err != nil {
		t.Fatalf("--version exited with error: %v\noutput: %s", err, output)
	}

	if output == "" {
		t.Error("expected non-empty output from --version")
	}

	// The injected version string must appear somewhere in the output.
	if !contains(output, wantVersion) {
		t.Errorf("expected %q in --version output, got: %s", wantVersion, output)
	}
}

// contains is a simple substring check to avoid importing strings in tests.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
