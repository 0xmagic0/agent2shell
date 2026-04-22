package main

import (
	"strings"
	"testing"
)

func TestClampExitCode(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero passes through", 0, 0},
		{"one passes through", 1, 1},
		{"125 passes through", 125, 125},
		{"126 clamps to 125", 126, 125},
		{"127 clamps to 125", 127, 125},
		{"255 clamps to 125", 255, 125},
		{"negative clamps to 126", -1, 126},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampExitCode(tt.input)
			if got != tt.expected {
				t.Errorf("clampExitCode(%d) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestArgJoining(t *testing.T) {
	args := []string{"ls", "-la", "/tmp"}
	got := strings.Join(args, " ")
	want := "ls -la /tmp"
	if got != want {
		t.Errorf("strings.Join = %q, want %q", got, want)
	}
}
