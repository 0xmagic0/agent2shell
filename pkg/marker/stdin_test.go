package marker_test

import (
	"strings"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/marker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Task 2.1: GenerateDelimiter ──────────────────────────────────────────────

// TestGenerateDelimiter_Format verifies the delimiter starts with "A2S_STDIN_"
// followed by exactly 8 lowercase hex characters.
func TestGenerateDelimiter_Format(t *testing.T) {
	delim := marker.GenerateDelimiter()
	assert.True(t, strings.HasPrefix(delim, "A2S_STDIN_"),
		"delimiter must start with A2S_STDIN_, got %q", delim)
	suffix := strings.TrimPrefix(delim, "A2S_STDIN_")
	assert.Len(t, suffix, 8, "delimiter suffix must be 8 chars, got %q", suffix)
	for _, c := range suffix {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"delimiter suffix must be lowercase hex, got char %q in %q", string(c), suffix)
	}
}

// TestGenerateDelimiter_Uniqueness verifies 1000 consecutive calls produce
// distinct delimiters.
func TestGenerateDelimiter_Uniqueness(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := range n {
		d := marker.GenerateDelimiter()
		_, dup := seen[d]
		require.False(t, dup, "call %d: GenerateDelimiter returned duplicate %q", i, d)
		seen[d] = struct{}{}
	}
}

// ─── Task 2.1: WrapStdinCommand ───────────────────────────────────────────────

// TestWrapStdinCommand_Format verifies the heredoc output is correctly
// structured: cat <<'DELIM' | cmd\n<content>\nDELIM
func TestWrapStdinCommand_Format(t *testing.T) {
	stdin := "id\nwhoami\n"
	got := marker.WrapStdinCommand("bash", stdin)

	// Must start with subshell wrapping and contain " | bash"
	assert.True(t, strings.HasPrefix(got, "(cat <<'A2S_STDIN_"),
		"must start with (cat <<'A2S_STDIN_, got %q", got)
	assert.Contains(t, got, "| bash")

	// Extract delimiter from first line
	lines := strings.SplitN(got, "\n", 2)
	require.Len(t, lines, 2, "output must have at least 2 lines")
	firstLine := lines[0]
	assert.Contains(t, firstLine, "A2S_STDIN_")

	// Last non-empty line must be closing paren (subshell)
	allLines := strings.Split(got, "\n")
	lastNonEmpty := ""
	for i := len(allLines) - 1; i >= 0; i-- {
		if allLines[i] != "" {
			lastNonEmpty = allLines[i]
			break
		}
	}
	assert.Equal(t, ")", lastNonEmpty,
		"last non-empty line must be closing paren, got %q", lastNonEmpty)

	// Content must appear between the first line and the closing delimiter
	assert.Contains(t, got, "id\n")
	assert.Contains(t, got, "whoami\n")
}

// TestWrapStdinCommand_EmptyStdin verifies that when stdin is empty, the
// command is returned unchanged.
func TestWrapStdinCommand_EmptyStdin(t *testing.T) {
	got := marker.WrapStdinCommand("bash", "")
	assert.Equal(t, "bash", got)
}

// TestWrapStdinCommand_EmptyStdinWithComplexCmd verifies passthrough for a
// complex command with no stdin.
func TestWrapStdinCommand_EmptyStdinWithComplexCmd(t *testing.T) {
	cmd := "cat /etc/passwd | grep root"
	got := marker.WrapStdinCommand(cmd, "")
	assert.Equal(t, cmd, got)
}

// TestWrapStdinCommand_SingleQuotes verifies stdin content with single quotes
// does NOT break the heredoc (heredoc body is literal, no quoting needed).
func TestWrapStdinCommand_SingleQuotes(t *testing.T) {
	stdin := "echo 'hello world'\n"
	got := marker.WrapStdinCommand("bash", stdin)
	assert.Contains(t, got, "echo 'hello world'")
	// Must still be valid structure
	assert.True(t, strings.HasPrefix(got, "(cat <<'A2S_STDIN_"))
}

// TestWrapStdinCommand_Backslashes verifies stdin with backslashes passes
// through unchanged inside the heredoc body.
func TestWrapStdinCommand_Backslashes(t *testing.T) {
	stdin := "echo \"hello\\nworld\"\n"
	got := marker.WrapStdinCommand("bash", stdin)
	assert.Contains(t, got, `echo "hello\nworld"`)
}

// TestWrapStdinCommand_MarkerLikeContent verifies stdin containing marker-like
// strings does not confuse the wrapping (the delimiter is uniquely generated).
func TestWrapStdinCommand_MarkerLikeContent(t *testing.T) {
	stdin := "echo '---A2S-START-deadbeef---'\necho '---A2S-END-deadbeef---0'\n"
	got := marker.WrapStdinCommand("bash", stdin)
	// Must still produce valid heredoc structure
	assert.True(t, strings.HasPrefix(got, "(cat <<'A2S_STDIN_"))
	assert.Contains(t, got, "---A2S-START-deadbeef---")
}

// TestWrapStdinCommand_PythonCommand verifies wrapping with a non-bash command.
func TestWrapStdinCommand_PythonCommand(t *testing.T) {
	stdin := "print('hello')\n"
	got := marker.WrapStdinCommand("python3", stdin)
	assert.True(t, strings.HasPrefix(got, "(cat <<'A2S_STDIN_"))
	assert.Contains(t, got, "| python3")
	assert.Contains(t, got, "print('hello')")
}
