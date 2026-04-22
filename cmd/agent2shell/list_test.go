package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

// --- filterByTag ---

func TestFilterByTag_EmptyTag(t *testing.T) {
	entries := []types.SessionEntry{
		{SessionInfo: types.SessionInfo{Tag: "web"}, SocketPath: "/tmp/a2s-1.sock"},
		{SessionInfo: types.SessionInfo{Tag: "db"}, SocketPath: "/tmp/a2s-2.sock"},
	}
	got := filterByTag(entries, "")
	if len(got) != len(entries) {
		t.Errorf("filterByTag with empty tag: got %d entries, want %d", len(got), len(entries))
	}
}

func TestFilterByTag_MatchingTag(t *testing.T) {
	entries := []types.SessionEntry{
		{SessionInfo: types.SessionInfo{Tag: "web"}, SocketPath: "/tmp/a2s-1.sock"},
		{SessionInfo: types.SessionInfo{Tag: "db"}, SocketPath: "/tmp/a2s-2.sock"},
		{SessionInfo: types.SessionInfo{Tag: "web"}, SocketPath: "/tmp/a2s-3.sock"},
	}
	got := filterByTag(entries, "web")
	if len(got) != 2 {
		t.Errorf("filterByTag with tag=web: got %d entries, want 2", len(got))
	}
	for _, e := range got {
		if e.Tag != "web" {
			t.Errorf("filterByTag returned entry with tag %q, want web", e.Tag)
		}
	}
}

func TestFilterByTag_NoMatch(t *testing.T) {
	entries := []types.SessionEntry{
		{SessionInfo: types.SessionInfo{Tag: "web"}, SocketPath: "/tmp/a2s-1.sock"},
	}
	got := filterByTag(entries, "db")
	if len(got) != 0 {
		t.Errorf("filterByTag with no match: got %d entries, want 0", len(got))
	}
}

// --- formatTable ---

func TestFormatTable_TwoEntries(t *testing.T) {
	entries := []types.SessionEntry{
		{
			SessionInfo: types.SessionInfo{
				RemoteAddr: "10.0.0.5:54321",
				User:       "www-data",
				Hostname:   "web-01",
				Tag:        "webserver",
			},
			SocketPath: "/tmp/a2s-1.sock",
		},
		{
			SessionInfo: types.SessionInfo{
				RemoteAddr: "10.0.0.8:41022",
				User:       "root",
				Hostname:   "db-01",
				Tag:        "database",
			},
			SocketPath: "/tmp/a2s-2.sock",
		},
	}

	var buf bytes.Buffer
	formatTable(&buf, entries)
	out := buf.String()

	// Header present
	if !strings.Contains(out, "SOCKET") {
		t.Errorf("output missing SOCKET header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "REMOTE") {
		t.Errorf("output missing REMOTE header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "USER") {
		t.Errorf("output missing USER header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "HOST") {
		t.Errorf("output missing HOST header\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "TAG") {
		t.Errorf("output missing TAG header\nfull output:\n%s", out)
	}

	// Entry 1
	if !strings.Contains(out, "/tmp/a2s-1.sock") {
		t.Errorf("output missing socket path for entry 1\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.5:54321") {
		t.Errorf("output missing remote addr for entry 1\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "www-data") {
		t.Errorf("output missing user for entry 1\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "web-01") {
		t.Errorf("output missing hostname for entry 1\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "webserver") {
		t.Errorf("output missing tag for entry 1\nfull output:\n%s", out)
	}

	// Entry 2
	if !strings.Contains(out, "/tmp/a2s-2.sock") {
		t.Errorf("output missing socket path for entry 2\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "root") {
		t.Errorf("output missing user for entry 2\nfull output:\n%s", out)
	}

	// Two data rows (lines after header)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 { // header + 2 data rows
		t.Errorf("expected at least 3 lines (header + 2 rows), got %d\nfull output:\n%s", len(lines), out)
	}
}

func TestFormatTable_EmptyTagColumn(t *testing.T) {
	entries := []types.SessionEntry{
		{
			SessionInfo: types.SessionInfo{
				RemoteAddr: "1.2.3.4:1234",
				User:       "root",
				Hostname:   "box",
				Tag:        "", // empty tag — no crash expected
			},
			SocketPath: "/tmp/a2s-1.sock",
		},
	}

	var buf bytes.Buffer
	// Must not panic
	formatTable(&buf, entries)
	out := buf.String()
	if !strings.Contains(out, "/tmp/a2s-1.sock") {
		t.Errorf("output missing socket path\nfull output:\n%s", out)
	}
}

// --- runList discovery mode ---

func makeSessionInfo(remote, user, host, tag string) *types.SessionInfo {
	return &types.SessionInfo{
		RemoteAddr: remote,
		User:       user,
		Hostname:   host,
		Tag:        tag,
		Shell:      "/bin/bash",
		OS:         "linux",
		Arch:       "amd64",
	}
}

func withDiscoverAndStatus(
	t *testing.T,
	paths []string,
	discoverErr error,
	statusResponses map[string]*types.SessionInfo,
	statusErrors map[string]error,
) func() {
	t.Helper()

	origDiscover := discoverFunc
	origStatus := statusFunc

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

	return func() {
		discoverFunc = origDiscover
		statusFunc = origStatus
	}
}

func TestRunList_TwoLiveSockets(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": makeSessionInfo("10.0.0.1:1111", "root", "host1", "web"),
		"/tmp/a2s-2.sock": makeSessionInfo("10.0.0.2:2222", "user", "host2", "db"),
	}
	cleanup := withDiscoverAndStatus(t, paths, nil, statusResponses, nil)
	defer cleanup()

	cmd := newListCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "/tmp/a2s-1.sock") {
		t.Errorf("output missing first socket\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/a2s-2.sock") {
		t.Errorf("output missing second socket\nfull output:\n%s", out)
	}
}

func TestRunList_OneLiveOneStale(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": makeSessionInfo("10.0.0.1:1111", "root", "host1", "web"),
	}
	statusErrors := map[string]error{
		"/tmp/a2s-2.sock": errors.New("connection refused"),
	}
	cleanup := withDiscoverAndStatus(t, paths, nil, statusResponses, statusErrors)
	defer cleanup()

	cmd := newListCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "/tmp/a2s-1.sock") {
		t.Errorf("output missing live socket\nfull output:\n%s", out)
	}

	errOut := errBuf.String()
	if !strings.Contains(errOut, "warning") {
		t.Errorf("expected warning in stderr for stale socket\nstderr:\n%s", errOut)
	}
	if !strings.Contains(errOut, "/tmp/a2s-2.sock") {
		t.Errorf("stderr warning should mention stale socket path\nstderr:\n%s", errOut)
	}
}

func TestRunList_NoSockets(t *testing.T) {
	cleanup := withDiscoverAndStatus(t, []string{}, nil, nil, nil)
	defer cleanup()

	cmd := newListCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "No active sessions") {
		t.Errorf("expected 'No active sessions' message\nfull output:\n%s", out)
	}
}

func TestRunList_TagFilter(t *testing.T) {
	paths := []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"}
	statusResponses := map[string]*types.SessionInfo{
		"/tmp/a2s-1.sock": makeSessionInfo("10.0.0.1:1111", "root", "host1", "web"),
		"/tmp/a2s-2.sock": makeSessionInfo("10.0.0.2:2222", "user", "host2", "db"),
	}
	cleanup := withDiscoverAndStatus(t, paths, nil, statusResponses, nil)
	defer cleanup()

	cmd := newListCmd()
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--tag", "web"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "/tmp/a2s-1.sock") {
		t.Errorf("output missing web-tagged socket\nfull output:\n%s", out)
	}
	if strings.Contains(out, "/tmp/a2s-2.sock") {
		t.Errorf("output should not contain db-tagged socket\nfull output:\n%s", out)
	}
}
