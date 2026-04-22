// Package detect provides pure parsing functions for probing a remote shell
// session's environment. Each function accepts raw command output and returns
// a normalized, canonical string value. No function returns an error; unknown
// or malformed input yields "unknown" or "".
//
// All functions are deterministic, free of side effects, and safe for
// concurrent use. This package imports only the Go standard library.
package detect

import "strings"

// canonicalShells is the set of recognized shell names.
var canonicalShells = map[string]struct{}{
	"bash": {},
	"zsh":  {},
	"sh":   {},
	"ash":  {},
	"dash": {},
	"fish": {},
}

// ParseShell normalizes echo $0 / $SHELL output to a canonical shell name.
//
// Processing steps:
//  1. Strip leading and trailing whitespace (including \r and \n).
//  2. Remove a leading '-' login-shell prefix if present (e.g. "-bash" → "bash").
//  3. Extract the basename after the last '/' (e.g. "/bin/bash" → "bash").
//  4. Match case-insensitively against the canonical set: bash, zsh, sh, ash, dash, fish.
//
// Returns the matched canonical name (lowercase), or "unknown" for any
// unrecognized input including empty string.
func ParseShell(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "unknown"
	}

	// Strip login-shell prefix.
	s = strings.TrimPrefix(s, "-")

	// Extract basename after last '/'.
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}

	s = strings.ToLower(s)

	if _, ok := canonicalShells[s]; ok {
		return s
	}
	return "unknown"
}

// canonicalOSes is the set of recognized operating system identifiers.
var canonicalOSes = map[string]struct{}{
	"linux":   {},
	"darwin":  {},
	"freebsd": {},
}

// ParseOS normalizes uname -s output to a lowercase OS identifier.
//
// Strips leading and trailing whitespace, converts to lowercase, and matches
// against the recognized set: linux, darwin, freebsd.
//
// Returns the lowercased OS name, or "unknown" for any unrecognized value
// including empty string.
func ParseOS(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if _, ok := canonicalOSes[s]; ok {
		return s
	}
	return "unknown"
}

// archMap maps raw uname -m values (lowercase) to Go architecture names.
var archMap = map[string]string{
	"x86_64":  "amd64",
	"aarch64": "arm64",
	"arm64":   "arm64",
	"armv7l":  "arm",
	"armv6l":  "arm",
	"armv5l":  "arm",
	"i386":    "386",
	"i486":    "386",
	"i586":    "386",
	"i686":    "386",
}

// ParseArch normalizes uname -m output to Go architecture convention.
//
// Strips leading and trailing whitespace, converts to lowercase, and applies
// the following mapping:
//   - x86_64          → "amd64"
//   - aarch64, arm64  → "arm64"
//   - armv7l, armv6l, armv5l → "arm"
//   - i386, i486, i586, i686 → "386"
//
// Returns "unknown" for any input not in the mapping, including empty string.
func ParseArch(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if canon, ok := archMap[s]; ok {
		return canon
	}
	return "unknown"
}

// extractValue scans os-release content for a "KEY=value" line and returns
// the value. It handles both LF and CRLF line endings, and strips surrounding
// double-quote characters if present. Returns ("", false) if the key is absent.
func extractValue(content, key string) (string, bool) {
	prefix := key + "="
	for _, line := range strings.Split(content, "\n") {
		// Strip CRLF.
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		val := line[len(prefix):]
		// Strip surrounding double quotes.
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		return val, true
	}
	return "", false
}

// ParseDistro extracts the distro name from /etc/os-release content.
//
// Searches raw line-by-line for PRETTY_NAME; falls back to NAME if absent.
// Handles both LF and CRLF line endings. Strips surrounding double-quote
// characters from the extracted value. Interior whitespace is preserved.
//
// Returns the extracted value, or "" if neither key is found.
func ParseDistro(raw string) string {
	if v, ok := extractValue(raw, "PRETTY_NAME"); ok {
		return v
	}
	if v, ok := extractValue(raw, "NAME"); ok {
		return v
	}
	return ""
}

// ParseUser extracts the username from POSIX id command output.
//
// Parses the format "uid=N(name) ..." and returns the name between the first
// '(' and ')' following "uid=".
//
// Returns "" if the input is malformed or empty.
func ParseUser(raw string) string {
	uidIdx := strings.Index(raw, "uid=")
	if uidIdx < 0 {
		return ""
	}
	rest := raw[uidIdx+len("uid="):]

	openIdx := strings.Index(rest, "(")
	if openIdx < 0 {
		return ""
	}
	closeIdx := strings.Index(rest[openIdx:], ")")
	if closeIdx < 0 {
		return ""
	}

	return rest[openIdx+1 : openIdx+closeIdx]
}

// ParseHostname strips all leading and trailing whitespace from hostname
// command output, including spaces, tabs, \r, and \n.
//
// Returns the stripped result, or "" for empty input. Interior characters
// are never modified.
func ParseHostname(raw string) string {
	return strings.TrimSpace(raw)
}
