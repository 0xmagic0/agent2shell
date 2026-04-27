package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

// broadcastResult holds the outcome of running a command on a single session.
type broadcastResult struct {
	SocketPath string `json:"socket_path"`
	Hostname   string `json:"hostname"`
	Tag        string `json:"tag,omitempty"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exit_code"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// runFunc is the client.Run function used to execute commands. Tests swap it.
var runFunc = client.Run

var broadcastCmd = &cobra.Command{
	Use:   "broadcast <command...>",
	Short: "Execute a command across multiple sessions",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runBroadcast,
}

func init() {
	broadcastCmd.Flags().String("tag", "", "filter sessions by tag")
	broadcastCmd.Flags().Bool("all", false, "target all active sessions")
	broadcastCmd.Flags().Bool("json", false, "output as JSON")
	broadcastCmd.Flags().IntP("timeout", "t", 30, "per-session timeout in seconds")
	broadcastCmd.Flags().Int("parallel", 10, "max concurrent executions")
	rootCmd.AddCommand(broadcastCmd)
}

func runBroadcast(cmd *cobra.Command, args []string) error {
	tag, _ := cmd.Flags().GetString("tag")        // flag registered in init()
	all, _ := cmd.Flags().GetBool("all")          // flag registered in init()
	asJSON, _ := cmd.Flags().GetBool("json")      // flag registered in init()
	timeout, _ := cmd.Flags().GetInt("timeout")   // flag registered in init()
	parallel, _ := cmd.Flags().GetInt("parallel") // flag registered in init()

	// Require at least one targeting mode.
	if !all && tag == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "error: specify --all or --tag to target sessions\n")
		return &exitError{code: 126}
	}

	paths, err := discoverFunc()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", err)
		return &exitError{code: 126}
	}

	// Collect valid session entries; warn and skip stale sockets.
	ctx := context.Background()
	var entries []types.SessionEntry
	for _, path := range paths {
		info, err := statusFunc(ctx, path)
		if err != nil {
			// Stale socket — warn but continue building the entry list.
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping stale socket %s\n", path)
			continue
		}
		entries = append(entries, types.SessionEntry{SessionInfo: *info, SocketPath: path})
	}

	// Apply tag filter when --all is not the sole selector.
	if tag != "" {
		entries = filterByTag(entries, tag)
	}

	if len(entries) == 0 {
		fmt.Fprint(cmd.OutOrStdout(), "No matching sessions.\n")
		return nil
	}

	command := strings.Join(args, " ")

	// Bounded concurrent execution — results slice is pre-allocated so each
	// goroutine writes to its own index without a mutex.
	concurrency := parallel
	if len(entries) < concurrency {
		concurrency = len(entries)
	}
	sem := make(chan struct{}, concurrency)

	results := make([]broadcastResult, len(entries))
	var wg sync.WaitGroup

	for i, entry := range entries {
		wg.Add(1)
		go func(idx int, sockPath, hostname, entryTag string) {
			defer wg.Done()

			sem <- struct{}{}        // acquire slot
			defer func() { <-sem }() // release slot

			execCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			start := time.Now()
			resp, runErr := runFunc(execCtx, sockPath, command, timeout, "")
			dur := time.Since(start).Milliseconds()

			r := broadcastResult{
				SocketPath: sockPath,
				Hostname:   hostname,
				Tag:        entryTag,
				DurationMS: dur,
			}
			if runErr != nil {
				r.Error = runErr.Error()
				r.ExitCode = 126
				// Partial output may still be available when the server returned
				// an exec-level error alongside output. Preserve it.
				if resp != nil {
					r.Output = resp.Output
					// Override exit code with the actual value when available.
					if resp.ExitCode != 0 {
						r.ExitCode = resp.ExitCode
					}
				}
			} else {
				r.Output = resp.Output
				r.ExitCode = resp.ExitCode
			}
			results[idx] = r
		}(i, entry.SocketPath, entry.Hostname, entry.Tag)
	}
	wg.Wait()

	if asJSON {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			// json.MarshalIndent only fails on non-marshallable types (e.g.
			// channels). broadcastResult contains only basic types, so this
			// path is unreachable in practice.
			return fmt.Errorf("broadcast: marshal: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	formatBroadcastResults(cmd.OutOrStdout(), results)
	return nil
}

// formatBroadcastResults writes human-readable output for all broadcast
// results to w. Each session is preceded by a "=== hostname [socket] ==="
// banner and followed by the exit code.
func formatBroadcastResults(w io.Writer, results []broadcastResult) {
	for _, r := range results {
		fmt.Fprintf(w, "=== %s [%s] ===\n", r.Hostname, r.SocketPath)
		if r.Error != "" {
			fmt.Fprintf(w, "error: %s\n", r.Error)
		} else {
			fmt.Fprint(w, r.Output)
			if len(r.Output) > 0 && r.Output[len(r.Output)-1] != '\n' {
				fmt.Fprintln(w)
			}
		}
		fmt.Fprintf(w, "[exit %d]\n\n", r.ExitCode)
	}
}
