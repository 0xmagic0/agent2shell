package marker_test

import (
	"regexp"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/marker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// S3.1: GenerateID produces exactly 8 lowercase hex chars.
func TestGenerateID_Format(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	id := marker.GenerateID()
	assert.Regexp(t, re, id, "GenerateID must return exactly 8 lowercase hex chars")
}

// S3.2: 1000 GenerateID calls all produce distinct values.
func TestGenerateID_Uniqueness(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	for i := range n {
		id := marker.GenerateID()
		require.Regexp(t, re, id, "call %d: GenerateID returned bad format", i)
		_, dup := seen[id]
		require.False(t, dup, "call %d: GenerateID returned duplicate id %q", i, id)
		seen[id] = struct{}{}
	}
}

// S3.3: WrapCommand with a real command produces the exact expected string.
func TestWrapCommand_Normal(t *testing.T) {
	got := marker.WrapCommand("a1b2c3d4", "whoami")
	want := "echo '---A2S-START-a1b2c3d4---'; whoami; echo '---A2S-END-a1b2c3d4---'$?"
	assert.Equal(t, want, got)
}

// S3.4: WrapCommand with empty cmd still produces valid framing.
func TestWrapCommand_EmptyCmd(t *testing.T) {
	got := marker.WrapCommand("a1b2c3d4", "")
	want := "echo '---A2S-START-a1b2c3d4---'; ; echo '---A2S-END-a1b2c3d4---'$?"
	assert.Equal(t, want, got)
}

// S3.5 – S3.7: ParseStartMarker table-driven tests.
func TestParseStartMarker(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		wantID string
		wantOK bool
	}{
		// S3.5
		{
			name:   "exact start marker",
			line:   "---A2S-START-a1b2c3d4---",
			wantID: "a1b2c3d4",
			wantOK: true,
		},
		// S3.6: substring must not match
		{
			name:   "start marker embedded in longer line",
			line:   "prefix ---A2S-START-a1b2c3d4--- suffix",
			wantID: "",
			wantOK: false,
		},
		// S3.7: end marker must not match start parser
		{
			name:   "end marker rejected by ParseStartMarker",
			line:   "---A2S-END-a1b2c3d4---0",
			wantID: "",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := marker.ParseStartMarker(tc.line)
			assert.Equal(t, tc.wantID, id)
			assert.Equal(t, tc.wantOK, ok)
		})
	}
}

// S3.8 – S3.13: ParseEndMarker table-driven tests.
func TestParseEndMarker(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		wantID       string
		wantExitCode int
		wantOK       bool
	}{
		// S3.8
		{
			name:         "exit code 0",
			line:         "---A2S-END-a1b2c3d4---0",
			wantID:       "a1b2c3d4",
			wantExitCode: 0,
			wantOK:       true,
		},
		// S3.9
		{
			name:         "exit code 127",
			line:         "---A2S-END-a1b2c3d4---127",
			wantID:       "a1b2c3d4",
			wantExitCode: 127,
			wantOK:       true,
		},
		// S3.10
		{
			name:         "exit code 255",
			line:         "---A2S-END-a1b2c3d4---255",
			wantID:       "a1b2c3d4",
			wantExitCode: 255,
			wantOK:       true,
		},
		// S3.11: negative exit code (signal)
		{
			name:         "exit code -1 (signal)",
			line:         "---A2S-END-a1b2c3d4----1",
			wantID:       "a1b2c3d4",
			wantExitCode: -1,
			wantOK:       true,
		},
		// S3.12: space before exit code must fail
		{
			name:         "space before exit code",
			line:         "---A2S-END-a1b2c3d4--- 0",
			wantID:       "",
			wantExitCode: 0,
			wantOK:       false,
		},
		// S3.13: start marker rejected by ParseEndMarker
		{
			name:         "start marker rejected by ParseEndMarker",
			line:         "---A2S-START-a1b2c3d4---",
			wantID:       "",
			wantExitCode: 0,
			wantOK:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, exitCode, ok := marker.ParseEndMarker(tc.line)
			assert.Equal(t, tc.wantID, id)
			assert.Equal(t, tc.wantExitCode, exitCode)
			assert.Equal(t, tc.wantOK, ok)
		})
	}
}

// S3.14 – S3.17: IsMarkerLine table-driven tests.
func TestIsMarkerLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		// S3.14
		{
			name: "start marker is a marker line",
			line: "---A2S-START-a1b2c3d4---",
			want: true,
		},
		// S3.15
		{
			name: "end marker is a marker line",
			line: "---A2S-END-a1b2c3d4---0",
			want: true,
		},
		// S3.16
		{
			name: "contains prefix but is not a marker",
			line: "contains ---A2S- but not a marker",
			want: false,
		},
		// S3.17
		{
			name: "empty string",
			line: "",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, marker.IsMarkerLine(tc.line))
		})
	}
}
