package main

import (
	"context"
	"fmt"
	"os"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/transfer"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull <remote-path> <local-path>",
	Short: "Pull a file from the target",
	Args:  cobra.ExactArgs(2),
	RunE:  runPull,
}

func init() {
	pullCmd.Flags().IntP("timeout", "t", 300, "transfer timeout in seconds")
	rootCmd.AddCommand(pullCmd)
}

// pullExecBuilder returns an ExecFunc for the given socket path.
// Package-level var so tests can inject a mock.
var pullExecBuilder func(socketPath string) transfer.ExecFunc = func(socketPath string) transfer.ExecFunc {
	return func(ctx context.Context, command string, timeout int) (*types.ExecResponse, error) {
		return client.Run(ctx, socketPath, command, timeout, "")
	}
}

func runPull(cmd *cobra.Command, args []string) error {
	remotePath := args[0]
	localPath := args[1]

	errW := cmd.ErrOrStderr()

	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(errW, "error: %s\n", err)
		return &exitError{code: 126}
	}

	exec := pullExecBuilder(socketPath)
	ctx := context.Background()
	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()

	// Encoder detection is required — pull cannot proceed without a base64 encoder.
	encoder, err := transfer.DetectEncoder(ctx, exec, timeout)
	if err != nil {
		fmt.Fprintf(errW, "error: no base64 encoder available on target: %s\n", err)
		return &exitError{code: 126}
	}

	checksummer, err := transfer.DetectChecksum(ctx, exec, timeout)
	if err != nil {
		fmt.Fprintf(errW, "error: checksum detection failed: %s\n", err)
		return &exitError{code: 126}
	}

	// Warn-and-proceed when no checksum tool is available.
	checksumVerified := checksummer != nil
	if !checksumVerified {
		fmt.Fprintf(errW, "[!] Warning: no checksum tool available on target, skipping verification\n")
	}

	fmt.Fprintf(errW, "[*] Pulling %s → %s...\n", remotePath, localPath)

	opts := transfer.PullOpts{
		Encoder:     encoder,
		Checksummer: checksummer,
		Timeout:     timeout,
		OnProgress:  makeProgressFunc(errW),
	}

	if err := transfer.Pull(ctx, exec, remotePath, localPath, opts); err != nil {
		fmt.Fprintf(errW, "\nerror: pull failed: %s\n", err)
		return &exitError{code: 126}
	}

	// Print final newline after progress line, then summary.
	fmt.Fprintln(errW)

	info, err := os.Stat(localPath)
	var sizeStr string
	if err == nil {
		sizeStr = humanSize(info.Size())
	} else {
		sizeStr = "unknown size"
	}

	if checksumVerified {
		fmt.Fprintf(errW, "[*] Transfer complete. Checksum verified. (%s)\n", sizeStr)
	} else {
		fmt.Fprintf(errW, "[*] Transfer complete. Checksum NOT verified. (%s)\n", sizeStr)
	}

	return nil
}

// humanSize formats byte counts as a human-readable string.
// Shared across the package (push.go delegates here).
func humanSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes < kb:
		return fmt.Sprintf("%d B", bytes)
	case bytes < mb:
		return fmt.Sprintf("%.2f KB", float64(bytes)/kb)
	case bytes < gb:
		return fmt.Sprintf("%.2f MB", float64(bytes)/mb)
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/gb)
	}
}
