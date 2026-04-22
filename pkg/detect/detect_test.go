package detect_test

import (
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/detect"
)

// TestParseUser covers S7.13–S7.14: username extraction from id output
// and malformed/empty inputs.
func TestParseUser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// S7.13 — extraction
		{"standard", "uid=33(www-data) gid=33(www-data) groups=33(www-data)", "www-data"},
		{"root", "uid=0(root) gid=0(root) groups=0(root)", "root"},
		{"minimal", "uid=0(root)", "root"},
		{"user with hyphen", "uid=1001(john-doe) gid=1001(john-doe)", "john-doe"},

		// S7.14 — malformed and empty
		{"empty", "", ""},
		{"no parens", "uid=33", ""},
		{"garbage", "not-id-output", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ParseUser(tt.input)
			if got != tt.expected {
				t.Errorf("ParseUser(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseHostname covers S7.15: whitespace stripping, CRLF, interior
// preservation, and empty input.
func TestParseHostname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"trailing newline", "target-01\n", "target-01"},
		{"CRLF", "target-01\r\n", "target-01"},
		{"leading and trailing spaces", "  host  ", "host"},
		{"interior preserved", "my host", "my host"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ParseHostname(tt.input)
			if got != tt.expected {
				t.Errorf("ParseHostname(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseDistro covers S7.10–S7.12: PRETTY_NAME/NAME extraction, CRLF,
// fallback logic, and empty/garbage inputs.
func TestParseDistro(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// S7.10 — PRETTY_NAME extraction
		{
			"quoted PRETTY_NAME",
			"PRETTY_NAME=\"Ubuntu 22.04.3 LTS\"\nNAME=\"Ubuntu\"",
			"Ubuntu 22.04.3 LTS",
		},
		{
			"unquoted PRETTY_NAME",
			"PRETTY_NAME=Alpine Linux v3.18",
			"Alpine Linux v3.18",
		},
		{
			"CRLF line endings",
			"PRETTY_NAME=\"Debian\"\r\nNAME=\"Debian\"",
			"Debian",
		},
		{
			"PRETTY_NAME first wins",
			"PRETTY_NAME=\"Arch\"\nNAME=\"Arch Linux\"",
			"Arch",
		},

		// S7.11 — fallback to NAME
		{
			"NAME only quoted",
			"ID=debian\nNAME=\"Debian GNU/Linux\"",
			"Debian GNU/Linux",
		},
		{
			"NAME only unquoted",
			"ID=alpine\nNAME=Alpine",
			"Alpine",
		},

		// S7.12 — empty and garbage
		{"empty", "", ""},
		{"no matching keys", "ID=debian\nVERSION_ID=11", ""},
		{"garbage", "xxxxxx", ""},

		// Additional edge case: PRETTY_NAME with interior whitespace preserved
		{
			"interior whitespace preserved",
			"PRETTY_NAME=\"My Cool Distro 1.0\"",
			"My Cool Distro 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ParseDistro(tt.input)
			if got != tt.expected {
				t.Errorf("ParseDistro(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseOS covers S7.6–S7.7: recognized OS names and unknown/empty inputs.
func TestParseOS(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// S7.6 — happy path
		{"Linux", "Linux", "linux"},
		{"Darwin", "Darwin", "darwin"},
		{"FreeBSD", "FreeBSD", "freebsd"},
		{"already lowercase", "linux", "linux"},
		{"trailing newline", "Linux\n", "linux"},

		// S7.7 — unknown and empty
		{"empty", "", "unknown"},
		{"Windows_NT", "Windows_NT", "unknown"},
		{"garbage", "SunOS", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ParseOS(tt.input)
			if got != tt.expected {
				t.Errorf("ParseOS(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseArch covers S7.8–S7.9: architecture mapping and unknown/empty inputs.
func TestParseArch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// S7.8 — mapping table
		{"x86_64", "x86_64", "amd64"},
		{"aarch64", "aarch64", "arm64"},
		{"arm64 canonical", "arm64", "arm64"},
		{"armv7l", "armv7l", "arm"},
		{"armv6l", "armv6l", "arm"},
		{"i686", "i686", "386"},
		{"i386", "i386", "386"},
		{"trailing newline", "x86_64\n", "amd64"},

		// S7.9 — unknown and empty
		{"empty", "", "unknown"},
		{"mips", "mips", "unknown"},
		{"garbage", "SPARC", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ParseArch(tt.input)
			if got != tt.expected {
				t.Errorf("ParseArch(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestDetectPackageCompiles verifies the package builds and all six
// exported functions exist with the correct signature.
func TestDetectPackageCompiles(t *testing.T) {
	// Calling each stub with empty string guarantees the function signatures
	// are correct and the package compiles.
	_ = detect.ParseShell("")
	_ = detect.ParseOS("")
	_ = detect.ParseArch("")
	_ = detect.ParseDistro("")
	_ = detect.ParseUser("")
	_ = detect.ParseHostname("")
}

// TestParseShell covers S7.1–S7.5: canonical names, full paths, login prefix,
// whitespace/CRLF, and unknown/empty inputs.
func TestParseShell(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// S7.1 — canonical names pass through
		{"bash verbatim", "bash", "bash"},
		{"zsh verbatim", "zsh", "zsh"},
		{"sh verbatim", "sh", "sh"},
		{"ash verbatim", "ash", "ash"},
		{"dash verbatim", "dash", "dash"},
		{"fish verbatim", "fish", "fish"},

		// S7.2 — full path extraction
		{"full path bash", "/bin/bash", "bash"},
		{"full path zsh", "/usr/bin/zsh", "zsh"},
		{"full path custom", "/usr/local/bin/zsh", "zsh"},
		{"full path sh", "/bin/sh", "sh"},

		// S7.3 — login shell prefix
		{"login bash", "-bash", "bash"},
		{"login zsh", "-zsh", "zsh"},
		{"login sh", "-sh", "sh"},

		// S7.4 — whitespace and CRLF
		{"trailing newline", "bash\n", "bash"},
		{"CRLF", "bash\r\n", "bash"},
		{"leading space", "  bash", "bash"},

		// S7.5 — unknown and empty
		{"empty", "", "unknown"},
		{"garbage", "garbage", "unknown"},
		{"powershell", "pwsh", "unknown"},
		{"numeric", "123", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ParseShell(tt.input)
			if got != tt.expected {
				t.Errorf("ParseShell(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}
