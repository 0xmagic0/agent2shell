package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

// newBroadcastTestCmd returns a fresh broadcast command with all flags
// registered, isolated from the global command tree.
func newBroadcastTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "broadcast",
		Args:         cobra.MinimumNArgs(1),
		RunE:         runBroadcast,
		SilenceUsage: true,
	}
	cmd.Flags().String("tag", "", "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().IntP("timeout", "t", 30, "")
	cmd.Flags().Int("parallel", 10, "")
	return cmd
}

// withBroadcastMocks swaps discoverFunc, statusFunc, and runFunc with mock
// implementations for the duration of a test. Returns a cleanup function.
func withBroadcastMocks(
	t *testing.T,
	paths []string,
	discoverErr error,
	statusResponses map[string]*types.SessionInfo,
	statusErrors map[string]error,
	runResponses map[string]*types.ExecResponse,
	runErrors map[string]error,
) func() {
	t.Helper()

	origDiscover := discoverFunc
	origStatus := statusFunc
	origRun := runFunc

	discoverFunc = func() ([]string, error) {
		return paths, discoverErr
	}
	statusFunc = func(ctx context.Context, path string) (*types.SessionInfo, error) {
		if statusErrors != nil {
			if err, ok := statusErrors[path]; ok {
				return nil, err
			}
		}
		if statusResponses != nil {
			if info, ok := statusResponses[path]; ok {
				return info, nil
			}
		}
		return nil, errors.New("unexpected path: " + path)
	}
	runFunc = func(ctx context.Context, sockPath, command string, timeout int, stdin string) (*types.ExecResponse, error) {
		if runErrors != nil {
			if err, ok := runErrors[sockPath]; ok {
				return nil, err
			}
		}
		if runResponses != nil {
			if resp, ok := runResponses[sockPath]; ok {
				return resp, nil
			}
		}
		return &types.ExecResponse{Output: "ok", ExitCode: 0}, nil
	}

	return func() {
		discoverFunc = origDiscover
		statusFunc = origStatus
		runFunc = origRun
	}
}

// TestBroadcast_NeitherAllNorTag verifies that omitting both --all and --tag
// returns an exitError{126} with an explanatory message on stderr.
func TestBroadcast_NeitherAllNorTag(t *testing.T) {
	cleanup := withBroadcastMocks(t, nil, nil, nil, nil, nil, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"id"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *exitError, got %T: %v", err, err)
	}
	if ee.code != 126 {
		t.Errorf("expected exit code 126, got %d", ee.code)
	}

	errOut := errBuf.String()
	if !strings.Contains(errOut, "--all") || !strings.Contains(errOut, "--tag") {
		t.Errorf("stderr should mention --all and --tag\nstderr: %s", errOut)
	}
}

// TestBroadcast_TwoSessionsBothSucceed verifies that two sessions both receive
// the command and their outputs appear in the results.
func TestBroadcast_TwoSessionsBothSucceed(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: "web"},
		"/tmp/a2s-2.sock": {Hostname: "db-01", Tag: "web"},
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "uid=0(root)", ExitCode: 0},
		"/tmp/a2s-2.sock": {Output: "uid=1000(user)", ExitCode: 0},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, runResponses, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--all", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "uid=0(root)") {
		t.Errorf("output missing web-01 result\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "uid=1000(user)") {
		t.Errorf("output missing db-01 result\nfull output:\n%s", out)
	}
}

// TestBroadcast_OneSessionFails verifies that a per-session error is recorded
// in the result and the overall command returns exit 0.
func TestBroadcast_OneSessionFails(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: ""},
		"/tmp/a2s-2.sock": {Hostname: "db-01", Tag: ""},
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "uid=0(root)", ExitCode: 0},
	}
	runErrors := map[string]error{
		"/tmp/a2s-2.sock": errors.New("connection timeout"),
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, runResponses, runErrors)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--all", "id"})

	// overall command must return nil (exit 0)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected nil error (exit 0), got: %v", err)
	}

	out := outBuf.String()
	// successful session present
	if !strings.Contains(out, "uid=0(root)") {
		t.Errorf("output missing successful session result\nfull output:\n%s", out)
	}
	// failed session error present
	if !strings.Contains(out, "connection timeout") {
		t.Errorf("output missing error for failed session\nfull output:\n%s", out)
	}
}

// TestBroadcast_TagFilter verifies that only sessions matching --tag are
// targeted; others are skipped.
func TestBroadcast_TagFilter(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: "web"},
		"/tmp/a2s-2.sock": {Hostname: "db-01", Tag: "db"},
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "web-output", ExitCode: 0},
		"/tmp/a2s-2.sock": {Output: "db-output", ExitCode: 0},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, runResponses, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--tag", "web", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "web-output") {
		t.Errorf("output missing web-tagged session result\nfull output:\n%s", out)
	}
	if strings.Contains(out, "db-output") {
		t.Errorf("output should NOT contain db-tagged session result\nfull output:\n%s", out)
	}
}

// TestBroadcast_NoMatchingSessions verifies that "No matching sessions." is
// printed and the command exits 0 when the tag filter eliminates all sessions.
func TestBroadcast_NoMatchingSessions(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: "web"},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, nil, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--tag", "nonexistent", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "No matching sessions") {
		t.Errorf("expected 'No matching sessions' message\nfull output:\n%s", out)
	}
}

// TestBroadcast_JSONOutput verifies that --json produces a valid JSON array of
// broadcastResult objects.
func TestBroadcast_JSONOutput(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: "web"},
		"/tmp/a2s-2.sock": {Hostname: "db-01", Tag: "web"},
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "uid=0(root)", ExitCode: 0},
		"/tmp/a2s-2.sock": {Output: "uid=1000(user)", ExitCode: 0},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, runResponses, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--all", "--json", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []broadcastResult
	if err := json.Unmarshal(outBuf.Bytes(), &results); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, outBuf.String())
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results in JSON, got %d", len(results))
	}
	for _, r := range results {
		if r.SocketPath == "" {
			t.Errorf("result missing socket_path: %+v", r)
		}
		if r.Hostname == "" {
			t.Errorf("result missing hostname: %+v", r)
		}
	}
}

// TestBroadcast_OrderPreservation verifies that results appear in the same
// order as discovered sockets regardless of goroutine scheduling.
func TestBroadcast_OrderPreservation(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock", "/tmp/a2s-3.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "host-1", Tag: ""},
		"/tmp/a2s-2.sock": {Hostname: "host-2", Tag: ""},
		"/tmp/a2s-3.sock": {Hostname: "host-3", Tag: ""},
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "out-1", ExitCode: 0},
		"/tmp/a2s-2.sock": {Output: "out-2", ExitCode: 0},
		"/tmp/a2s-3.sock": {Output: "out-3", ExitCode: 0},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, runResponses, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--all", "--json", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []broadcastResult
	if err := json.Unmarshal(outBuf.Bytes(), &results); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, want := range paths {
		if results[i].SocketPath != want {
			t.Errorf("result[%d].SocketPath = %q, want %q", i, results[i].SocketPath, want)
		}
	}
}

// TestBroadcast_HumanFormat verifies the "=== hostname [socket] ===" header
// pattern in the default (non-JSON) output.
func TestBroadcast_HumanFormat(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: ""},
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "uid=0(root)", ExitCode: 0},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, nil, runResponses, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--all", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "=== web-01") {
		t.Errorf("output missing hostname header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/a2s-1.sock") {
		t.Errorf("output missing socket path in header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "uid=0(root)") {
		t.Errorf("output missing command output\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "[exit 0]") {
		t.Errorf("output missing exit code\nfull output:\n%s", out)
	}
}

// TestBroadcast_DiscoverError verifies that a discovery error propagates as a
// non-zero exit.
func TestBroadcast_DiscoverError(t *testing.T) {
	cleanup := withBroadcastMocks(t, nil, errors.New("discover failed"), nil, nil, nil, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--all", "id"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from discover failure, got nil")
	}
}

// TestBroadcast_StaleSocketSkipped verifies that a session where statusFunc
// returns an error is skipped with a stderr warning but does not abort the
// whole command.
func TestBroadcast_StaleSocketSkipped(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": {Hostname: "web-01", Tag: ""},
	}
	statusErrors := map[string]error{
		"/tmp/a2s-2.sock": errors.New("stale socket"),
	}
	runResponses := map[string]*types.ExecResponse{
		"/tmp/a2s-1.sock": {Output: "uid=0(root)", ExitCode: 0},
	}

	cleanup := withBroadcastMocks(t, paths, nil, statusResponses, statusErrors, runResponses, nil)
	defer cleanup()

	cmd := newBroadcastTestCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--all", "id"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "uid=0(root)") {
		t.Errorf("output missing live session result\nfull output:\n%s", out)
	}

	errOut := errBuf.String()
	if !strings.Contains(errOut, "warning") {
		t.Errorf("stderr should contain warning for stale socket\nstderr: %s", errOut)
	}
}

// TestFormatBroadcastResults verifies the formatBroadcastResults helper
// directly via an io.Writer.
func TestFormatBroadcastResults(t *testing.T) {
	results := []broadcastResult{
		{
			SocketPath: "/tmp/a2s-1.sock",
			Hostname:   "web-01",
			Tag:        "web",
			Output:     "uid=0(root)\n",
			ExitCode:   0,
			DurationMS: 42,
		},
		{
			SocketPath: "/tmp/a2s-2.sock",
			Hostname:   "db-01",
			Tag:        "",
			Error:      "connection timeout",
			ExitCode:   126,
			DurationMS: 5000,
		},
	}

	var buf bytes.Buffer
	formatBroadcastResults(&buf, results)
	out := buf.String()

	if !strings.Contains(out, "=== web-01") {
		t.Errorf("output missing web-01 header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "=== db-01") {
		t.Errorf("output missing db-01 header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "uid=0(root)") {
		t.Errorf("output missing command output\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "connection timeout") {
		t.Errorf("output missing error message\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "[exit 0]") {
		t.Errorf("output missing exit 0\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "[exit 126]") {
		t.Errorf("output missing exit 126\nfull output:\n%s", out)
	}
}
