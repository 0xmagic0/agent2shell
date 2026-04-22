package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

var statusFunc = client.Status

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List active sessions",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func init() {
	listCmd.Flags().Bool("json", false, "output raw JSON")
	listCmd.Flags().String("tag", "", "filter by tag")
	listCmd.Flags().IntP("timeout", "t", 10, "per-socket IPC timeout in seconds")
	rootCmd.AddCommand(listCmd)
}

// newListCmd returns a fresh listCmd for tests that need isolated flag state.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List active sessions",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runList,
	}
	cmd.Flags().Bool("json", false, "output raw JSON")
	cmd.Flags().String("tag", "", "filter by tag")
	cmd.Flags().IntP("timeout", "t", 10, "per-socket IPC timeout in seconds")
	return cmd
}

func runList(cmd *cobra.Command, _ []string) error {
	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	socketPath, _ := cmd.Root().PersistentFlags().GetString("socket") // flag registered in root.go init()

	var entries []types.SessionEntry

	if socketPath != "" {
		// Mode A: explicit socket via -s flag
		info, err := statusFunc(ctx, socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
			return &exitError{code: 126}
		}
		entries = []types.SessionEntry{{SessionInfo: *info, SocketPath: socketPath}}
	} else {
		// Mode B: discover all sockets
		paths, err := discoverFunc()
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		for _, path := range paths {
			info, err := statusFunc(ctx, path)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping stale socket %s\n", path)
				continue
			}
			entries = append(entries, types.SessionEntry{SessionInfo: *info, SocketPath: path})
		}
	}

	tag, _ := cmd.Flags().GetString("tag") // flag registered in init()
	entries = filterByTag(entries, tag)

	asJSON, _ := cmd.Flags().GetBool("json") // flag registered in init()
	if asJSON {
		if entries == nil {
			entries = []types.SessionEntry{}
		}
		out, err := json.MarshalIndent(types.SessionsResponse{Sessions: entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("list: marshal: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	if len(entries) == 0 {
		fmt.Fprint(cmd.OutOrStdout(), "No active sessions.\n")
		return nil
	}

	formatTable(cmd.OutOrStdout(), entries)
	return nil
}

// filterByTag returns entries unchanged when tag is empty, otherwise returns
// only entries whose Tag field matches exactly.
func filterByTag(entries []types.SessionEntry, tag string) []types.SessionEntry {
	if tag == "" {
		return entries
	}
	out := entries[:0:0] // nil-safe empty slice with no allocation overlap
	for _, e := range entries {
		if e.Tag == tag {
			out = append(out, e)
		}
	}
	return out
}

// formatTable writes a tabwriter-aligned table of sessions to w.
func formatTable(w io.Writer, entries []types.SessionEntry) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  #\tSOCKET\tREMOTE\tUSER\tHOST\tTAG")
	for i, e := range entries {
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			e.SocketPath,
			e.RemoteAddr,
			e.User,
			e.Hostname,
			e.Tag,
		)
	}
	tw.Flush()
}
