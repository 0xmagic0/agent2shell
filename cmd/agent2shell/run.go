package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [command...]",
	Short: "Execute a command on the target",
	Args:  cobra.ArbitraryArgs,
	RunE:  runRun,
}

func init() {
	runCmd.Flags().IntP("timeout", "t", 30, "command timeout in seconds")
	runCmd.Flags().Bool("no-stream", false, "disable streaming output (buffer and print all at once)")
	runCmd.Flags().StringP("stdin", "i", "", "pipe a local file as stdin to the remote command (use - for OS stdin)")
	runCmd.Flags().SetInterspersed(false)
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return &exitError{code: 126}
	}

	stdinPath, _ := cmd.Flags().GetString("stdin")

	// Determine command: when --stdin is set and no args given, default to "bash".
	var command string
	if len(args) > 0 {
		command = strings.Join(args, " ")
	} else if stdinPath != "" {
		command = "bash"
	} else {
		fmt.Fprintf(os.Stderr, "error: requires at least 1 arg(s), only received 0\n")
		return &exitError{code: 126}
	}

	timeout, _ := cmd.Flags().GetInt("timeout")     // flag registered in init()
	noStream, _ := cmd.Flags().GetBool("no-stream") // flag registered in init()

	// Read stdin content if --stdin was specified.
	var stdinContent string
	if stdinPath != "" {
		stdinContent, err = readStdinContent(stdinPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			return &exitError{code: 126}
		}
	}

	// Context deadline must exceed the exec timeout so the client can receive
	// the server's timeout response before the context cancels the socket read.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second+5*time.Second)
	defer cancel()

	if noStream {
		return runBuffered(ctx, socketPath, command, timeout, stdinContent)
	}
	return runStreaming(ctx, socketPath, command, timeout, stdinContent)
}

// readStdinContent reads the content to pipe as stdin to the remote command.
// When path is "-", it reads from OS stdin. Otherwise it reads the named file.
// Returns an error with the file path included when the file cannot be read.
func readStdinContent(path string) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", path, err)
	}
	return string(data), nil
}

// runStreaming executes command via client.StreamRun, printing each line to
// stdout as it arrives.
func runStreaming(ctx context.Context, socketPath, command string, timeout int, stdin string) error {
	resp, err := client.StreamRun(ctx, socketPath, command, timeout, stdin, func(line string) {
		fmt.Println(line)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return &exitError{code: exitCodeForError(err)}
	}
	return &exitError{code: clampExitCode(resp.ExitCode)}
}

// runBuffered executes command via client.Run, printing the complete output
// after the command returns.
func runBuffered(ctx context.Context, socketPath, command string, timeout int, stdin string) error {
	resp, err := client.Run(ctx, socketPath, command, timeout, stdin)
	if err != nil {
		prefix := "error"
		if resp != nil {
			prefix = "exec error"
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", prefix, err)
		return &exitError{code: exitCodeForError(err)}
	}

	output := resp.Output
	if output != "" && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	fmt.Print(output)
	return &exitError{code: clampExitCode(resp.ExitCode)}
}

// exitCodeForError returns 124 for timeout errors and 126 for everything else.
// 124 matches GNU coreutils `timeout` convention.
func exitCodeForError(err error) int {
	if errors.Is(err, client.ErrExecTimeout) {
		return 124
	}
	return 126
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
