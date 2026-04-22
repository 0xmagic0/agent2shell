package transfer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "/tmp/file.txt",
			expected: "'/tmp/file.txt'",
		},
		{
			name:     "path with spaces",
			input:    "/tmp/my file.txt",
			expected: "'/tmp/my file.txt'",
		},
		{
			name:     "path with single quote",
			input:    "/tmp/it's here",
			expected: "'/tmp/it'\\''s here'",
		},
		{
			name:     "path with dollar sign",
			input:    "/tmp/$HOME/file",
			expected: "'/tmp/$HOME/file'",
		},
		{
			name:     "path with backticks",
			input:    "/tmp/`cmd`",
			expected: "'/tmp/`cmd`'",
		},
		{
			name:     "path with semicolon injection",
			input:    "/tmp/a;rm -rf /",
			expected: "'/tmp/a;rm -rf /'",
		},
		{
			name:     "path with double quote",
			input:    `/tmp/a"b`,
			expected: `'/tmp/a"b'`,
		},
		{
			name:     "path with newline",
			input:    "/tmp/file\nname",
			expected: "'/tmp/file\nname'",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shellQuote(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
