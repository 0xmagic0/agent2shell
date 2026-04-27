package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/transfer"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push <local-path> <remote-path>",
	Short: "Push a file to the target",
	Args:  cobra.ExactArgs(2),
	RunE:  runPush,
}

func init() {
	pushCmd.Flags().IntP("timeout", "t", 300, "transfer timeout in seconds")
	rootCmd.AddCommand(pushCmd)
}

// pushExecBuilder returns an ExecFunc that delegates to client.Run over the
// given Unix socket. Package-level var so tests can inject a mock.
var pushExecBuilder func(socketPath string) transfer.ExecFunc = buildPushExecFunc

// buildPushExecFunc returns an ExecFunc that delegates to client.Run over the
// given Unix socket. Extracted so tests can verify the closure without a live
// socket.
func buildPushExecFunc(socketPath string) transfer.ExecFunc {
	return func(ctx context.Context, cmd string, timeout int) (*types.ExecResponse, error) {
		return client.Run(ctx, socketPath, cmd, timeout, "")
	}
}

func runPush(cmd *cobra.Command, args []string) error {
	localPath := args[0]
	remotePath := args[1]

	errW := cmd.ErrOrStderr()

	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(errW, "error: %s\n", err)
		return &exitError{code: 126}
	}

	exec := pushExecBuilder(socketPath)
	ctx := context.Background()

	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Fprintf(errW, "error: stat %s: %s\n", localPath, err)
		return &exitError{code: 126}
	}

	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()

	decoder, err := transfer.DetectDecoder(ctx, exec, timeout)
	if err != nil {
		fmt.Fprintf(errW, "error: %s\n", err)
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

	size := info.Size()
	fmt.Fprintf(errW, "[*] Pushing %s → %s (%s)...\n",
		filepath.Base(localPath), remotePath, humanSize(size))

	opts := transfer.PushOpts{
		Decoder:     decoder,
		Checksummer: checksummer,
		Timeout:     timeout,
		OnProgress:  makeProgressFunc(errW),
	}

	if err := transfer.Push(ctx, exec, localPath, remotePath, opts); err != nil {
		fmt.Fprintf(errW, "\nerror: push failed: %s\n", err)
		return &exitError{code: 126}
	}

	// Move past the in-place progress line.
	fmt.Fprintf(errW, "\n")

	if checksumVerified {
		fmt.Fprintf(errW, "[*] Transfer complete. Checksum verified.\n")
	} else {
		fmt.Fprintf(errW, "[*] Transfer complete. Checksum NOT verified.\n")
	}

	return nil
}

// makeProgressFunc returns an OnProgress callback that prints to w.
func makeProgressFunc(w io.Writer) transfer.ProgressFunc {
	return func(transferred, total int64) {
		var percent int64
		if total > 0 {
			percent = transferred * 100 / total
		}
		fmt.Fprintf(w, "\r[*] %d%% (%s / %s)",
			percent, humanSize(transferred), humanSize(total))
	}
}
