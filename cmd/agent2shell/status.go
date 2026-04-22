package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/client"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show session metadata",
	Args:  cobra.NoArgs,
	RunE:  runStatus,
}

func init() {
	statusCmd.Flags().Bool("json", false, "output raw JSON")
	statusCmd.Flags().IntP("timeout", "t", 10, "IPC timeout in seconds")
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	socketPath, err := resolveSocket(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &exitError{code: 126}
	}

	timeout, _ := cmd.Flags().GetInt("timeout") // flag registered in init()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	info, err := client.Status(ctx, socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &exitError{code: 126}
	}

	asJSON, _ := cmd.Flags().GetBool("json") // flag registered in init()
	if asJSON {
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: marshal: %v\n", err)
			return &exitError{code: 126}
		}
		fmt.Printf("%s\n", data)
		return nil
	}

	formatStatus(os.Stdout, info)
	return nil
}

// formatStatus writes a human-readable session summary to w.
func formatStatus(w io.Writer, info *types.SessionInfo) {
	const col = "%-12s"

	fmt.Fprintf(w, col+" %s\n", "Remote:", info.RemoteAddr)
	fmt.Fprintf(w, col+" %s\n", "Shell:", info.Shell)
	fmt.Fprintf(w, col+" %s\n", "User:", info.User)
	fmt.Fprintf(w, col+" %s\n", "Hostname:", info.Hostname)
	fmt.Fprintf(w, col+" %s\n", "OS:", info.OS)
	fmt.Fprintf(w, col+" %s\n", "Arch:", info.Arch)

	if info.Distro != "" {
		fmt.Fprintf(w, col+" %s\n", "Distro:", info.Distro)
	}

	rel := formatDuration(time.Since(info.ConnectedAt))
	fmt.Fprintf(w, col+" %s (%s)\n", "Connected:", info.ConnectedAt.UTC().Format(time.RFC3339), rel)

	fmt.Fprintf(w, col+" %d\n", "Commands:", info.CommandsExecuted)

	if info.Tag != "" {
		fmt.Fprintf(w, col+" %s\n", "Tag:", info.Tag)
	}

	recording := "no"
	if info.Recording {
		recording = "yes"
	}
	fmt.Fprintf(w, col+" %s\n", "Recording:", recording)
}

// formatDuration returns a human-readable relative duration (e.g. "2h15m0s ago").
func formatDuration(d time.Duration) string {
	return d.Truncate(time.Second).String() + " ago"
}
