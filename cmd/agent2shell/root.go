// Package main defines the root Cobra command for agent2shell.
package main

import (
	"github.com/spf13/cobra"
)

// rootCmd is the top-level command. All subcommands register themselves here.
var rootCmd = &cobra.Command{
	Use:   "agent2shell",
	Short: "Programmable bridge between AI agents and reverse shells.",
	Long: `agent2shell turns raw reverse shell sessions into structured JSON APIs
exposed over Unix domain sockets. Any AI agent or tool can connect through
the socket and execute commands, transfer files, and inspect session state
without touching the raw shell stream directly.`,

	// SilenceErrors prevents Cobra from printing the error itself; main.go
	// handles the exit code so the caller sees exactly 127 on CLI errors.
	SilenceErrors: true,

	// SilenceUsage prevents usage being printed on every error — only show
	// it when the user explicitly asks (--help).
	SilenceUsage: true,

	// Args rejects any positional arguments so that unknown subcommands
	// (e.g. "agent2shell unknowncmd") return an error and exit 127.
	Args: cobra.NoArgs,

	// RunE is required so that Cobra executes the command (and therefore
	// enforces Args validation). Without a Run function Cobra treats root
	// as a pure-parent group and skips arg validation, allowing unknown
	// positional args to slip through without error. Printing help here
	// is the correct no-arg behaviour.
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Expose the version string via the built-in --version flag so that
	// `agent2shell --version` prints the value injected at build time.
	rootCmd.Version = Version

	// PersistentFlags are inherited by every subcommand.
	rootCmd.PersistentFlags().StringP("socket", "s", "", "Unix socket path")
	rootCmd.PersistentFlags().Bool("no-color", false, "disable colored output")
}
