// Package marker implements the double-marker protocol used to delimit command
// output in a reverse-shell session.  Each command is wrapped with a unique
// start and end marker so that the session reader can unambiguously detect
// where output begins and where it ends (along with the exit code).
package marker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// MarkerPrefix is the common prefix shared by every marker line.
const MarkerPrefix = "---A2S-"

// StartTag is the tag component embedded in a start marker.
const StartTag = "START"

// EndTag is the tag component embedded in an end marker.
const EndTag = "END"

// startPrefix is the full prefix of a start-marker line.
const startPrefix = MarkerPrefix + StartTag + "-" // ---A2S-START-

// endPrefix is the full prefix of an end-marker line.
const endPrefix = MarkerPrefix + EndTag + "-" // ---A2S-END-

// closingSuffix is the three-dash delimiter that terminates the ID segment in
// both marker types.
const closingSuffix = "---"

// GenerateID returns exactly 8 lowercase hexadecimal characters derived from
// the first segment of a random UUID v4.  Every call produces a different
// value.
func GenerateID() string {
	id := uuid.New().String() // xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	return id[:8]             // first segment is always 8 hex chars
}

// WrapCommand wraps cmd in start/end marker echoes so the session reader can
// detect its output boundaries and exit code.  The caller is responsible for
// any shell-safety concerns; cmd is embedded verbatim.
//
// Format:
//
//	echo '---A2S-START-<id>---'; <cmd>; echo '---A2S-END-<id>---'$?
func WrapCommand(id, cmd string) string {
	return fmt.Sprintf(
		"echo '%s%s%s'; %s; echo '%s%s%s'$?",
		startPrefix, id, closingSuffix,
		cmd,
		endPrefix, id, closingSuffix,
	)
}

// ParseStartMarker reports whether line is exactly a start marker.  When it
// is, it returns the embedded ID and true; otherwise it returns ("", false).
//
// A valid start marker has the form:  ---A2S-START-<id>---
func ParseStartMarker(line string) (id string, ok bool) {
	// Must start with ---A2S-START- and end with ---
	if !strings.HasPrefix(line, startPrefix) || !strings.HasSuffix(line, closingSuffix) {
		return "", false
	}
	// Extract what is between the prefix and the closing ---
	inner := line[len(startPrefix) : len(line)-len(closingSuffix)]
	// inner must be non-empty and must not itself contain dashes (no extra
	// structure — the closing --- is already stripped).
	if inner == "" {
		return "", false
	}
	// Verify the reconstruction is an exact match (guards against e.g.
	// "---A2S-START----" where inner would be "-").
	want := startPrefix + inner + closingSuffix
	if line != want {
		return "", false
	}
	return inner, true
}

// ParseEndMarker reports whether line is exactly an end marker.  When it is,
// it returns the embedded ID, the exit code, and true; otherwise it returns
// ("", 0, false).
//
// A valid end marker has the form:  ---A2S-END-<id>---<exitCode>
// Exit code is directly appended after the closing --- with no space.
// Negative values (e.g. -1 for signals) are supported.
func ParseEndMarker(line string) (id string, exitCode int, ok bool) {
	if !strings.HasPrefix(line, endPrefix) {
		return "", 0, false
	}

	// Everything after ---A2S-END- is "<id>---<exitCode>"
	rest := line[len(endPrefix):]

	// Find the closing --- that separates the id from the exit code.
	// The id itself contains only hex chars (no dashes), so the first
	// occurrence of "---" in rest is the delimiter.
	delimIdx := strings.Index(rest, closingSuffix)
	if delimIdx < 0 {
		return "", 0, false
	}

	id = rest[:delimIdx]
	if id == "" {
		return "", 0, false
	}

	exitStr := rest[delimIdx+len(closingSuffix):]

	// exitStr must be a valid integer with no leading/trailing whitespace.
	// strconv.Atoi rejects spaces, so this naturally handles S3.12.
	code, err := strconv.Atoi(exitStr)
	if err != nil {
		return "", 0, false
	}

	// Final exact-match guard: reconstruct and compare.
	want := endPrefix + id + closingSuffix + exitStr
	if line != want {
		return "", 0, false
	}

	return id, code, true
}

// IsMarkerLine returns true iff line is either a valid start marker or a valid
// end marker.  Lines that merely contain the marker prefix but do not have the
// correct structure return false.
func IsMarkerLine(line string) bool {
	if _, ok := ParseStartMarker(line); ok {
		return true
	}
	if _, _, ok := ParseEndMarker(line); ok {
		return true
	}
	return false
}
