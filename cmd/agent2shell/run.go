package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [command...]",
	Short: "Execute a command on the target",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRun,
}

func init() {
	runCmd.Flags().IntP("timeout", "t", 30, "command timeout in seconds")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return &exitError{code: 126}
	}

	command := strings.Join(args, " ")

	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	resp, err := client.Run(ctx, socketPath, command, timeout)
	if err != nil {
		prefix := "error"
		if resp != nil {
			prefix = "exec error"
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", prefix, err)
		return &exitError{code: 126}
	}

	fmt.Print(resp.Output)
	return &exitError{code: clampExitCode(resp.ExitCode)}
}

// clampExitCode maps remote exit codes to the agent2shell exit code contract:
//   - negative  → 126 (agent2shell error sentinel)
//   - 0–125     → forwarded as-is
//   - 126–255   → clamped to 125 (avoids collision with agent2shell sentinels)
func clampExitCode(code int) int {
	if code < 0 {
		return 126
	}
	if code > 125 {
		return 125
	}
	return code
}
