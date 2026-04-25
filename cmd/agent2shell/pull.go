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

func runPull(cmd *cobra.Command, args []string) error {
	remotePath := args[0]
	localPath := args[1]

	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return &exitError{code: 126}
	}

	exec := func(ctx context.Context, command string, timeout int) (*types.ExecResponse, error) {
		return client.Run(ctx, socketPath, command, timeout, "")
	}

	ctx := context.Background()
	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()

	checksummer, err := transfer.DetectChecksum(ctx, exec, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: checksum detection failed: %s\n", err)
		return &exitError{code: 126}
	}
	if checksummer == nil {
		fmt.Fprintf(os.Stderr, "error: no checksum tool (md5sum/md5) available on target\n")
		return &exitError{code: 126}
	}

	fmt.Fprintf(os.Stderr, "[*] Pulling %s → %s...\n", remotePath, localPath)

	opts := transfer.PullOpts{
		Checksummer: checksummer,
		Timeout:     timeout,
		OnProgress: func(transferred, total int64) {
			pct := int64(0)
			if total > 0 {
				pct = transferred * 100 / total
			}
			fmt.Fprintf(os.Stderr, "\r[*] %d%% (%s / %s)",
				pct, humanSize(transferred), humanSize(total))
		},
	}

	if err := transfer.Pull(ctx, exec, remotePath, localPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: pull failed: %s\n", err)
		return &exitError{code: 126}
	}

	// Print final newline after progress line, then summary.
	fmt.Fprintln(os.Stderr)

	info, err := os.Stat(localPath)
	var sizeStr string
	if err == nil {
		sizeStr = humanSize(info.Size())
	} else {
		// stat failure after a successful pull is unexpected; surface size as
		// unknown rather than masking the error with a panic or silent zero.
		sizeStr = "unknown size"
	}

	fmt.Fprintf(os.Stderr, "[*] Transfer complete. Checksum verified. (%s)\n", sizeStr)

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
