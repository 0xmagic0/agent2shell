package main

import (
	"context"
	"fmt"
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

	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return &exitError{code: 126}
	}

	exec := buildPushExecFunc(socketPath)
	ctx := context.Background()

	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: stat %s: %s\n", localPath, err)
		return &exitError{code: 126}
	}

	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()

	decoder, err := transfer.DetectDecoder(ctx, exec, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return &exitError{code: 126}
	}

	checksummer, err := transfer.DetectChecksum(ctx, exec, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: checksum detection failed: %s\n", err)
		return &exitError{code: 126}
	}
	if checksummer == nil {
		fmt.Fprintf(os.Stderr, "error: no checksum tool (md5sum/md5) available on target\n")
		return &exitError{code: 126}
	}

	size := info.Size()
	fmt.Fprintf(os.Stderr, "[*] Pushing %s → %s (%s)...\n",
		filepath.Base(localPath), remotePath, humanSize(size))

	opts := transfer.PushOpts{
		Decoder:     decoder,
		Checksummer: checksummer,
		Timeout:     timeout,
		OnProgress: func(transferred, total int64) {
			var percent int64
			if total > 0 {
				percent = transferred * 100 / total
			}
			fmt.Fprintf(os.Stderr, "\r[*] %d%% (%s / %s)",
				percent, humanSize(transferred), humanSize(total))
		},
	}

	if err := transfer.Push(ctx, exec, localPath, remotePath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: push failed: %s\n", err)
		return &exitError{code: 126}
	}

	// Move past the in-place progress line.
	fmt.Fprintf(os.Stderr, "\n")

	fmt.Fprintf(os.Stderr, "[*] Transfer complete. Checksum verified.\n")

	return nil
}
