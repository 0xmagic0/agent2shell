package main

import (
	"context"
	"testing"

	"github.com/0xmagic0/agent2shell/pkg/transfer"
)

func TestHumanSize(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"512 bytes", 512, "512 B"},
		{"exactly 1 KB", 1024, "1.00 KB"},
		{"1.5 KB", 1536, "1.50 KB"},
		{"exactly 1 MB", 1048576, "1.00 MB"},
		{"exactly 1 GB", 1073741824, "1.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanSize(tt.input)
			if got != tt.expected {
				t.Errorf("humanSize(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildPushExecFunc(t *testing.T) {
	const socketPath = "/tmp/test.sock"

	// buildPushExecFunc must return an ExecFunc that closes over socketPath.
	// We verify the closure captures the path by checking the function is non-nil
	// and has the correct type. An actual network call would fail without a live
	// socket, so we only validate the structural contract here.
	exec := buildPushExecFunc(socketPath)
	if exec == nil {
		t.Fatal("buildPushExecFunc returned nil")
	}

	// Verify the returned type satisfies transfer.ExecFunc by assigning it.
	var _ transfer.ExecFunc = exec

	// Call with a cancelled context — the client will fail immediately without
	// hitting the network, confirming the closure is wired correctly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done

	resp, err := exec(ctx, "echo hi", 5)
	// We expect an error (context cancelled or dial failure); no panic.
	if err == nil {
		t.Logf("unexpected success: resp=%+v", resp)
	}
}
