// Package main is the entry point for the agent2shell CLI.
// Version, Commit, and BuildDate are injected at build time via -ldflags -X.
// Zero values are valid — the binary runs normally without them.
package main

import (
	"os"
)

// Version is the release tag, injected via -ldflags -X main.Version=<value>.
var Version string

// Commit is the git short hash, injected via -ldflags -X main.Commit=<value>.
var Commit string

// BuildDate is the ISO-8601 build timestamp, injected via
// -ldflags -X main.BuildDate=<value>.
var BuildDate string

func main() {
	if err := rootCmd.Execute(); err != nil {
		// Cobra has already printed the error; exit 127 for CLI errors
		// (unknown command, bad flag, etc.) to follow Unix conventions.
		os.Exit(127)
	}
}
