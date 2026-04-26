// Package main is the entry point for the agent2shell CLI.
// Version, Commit, and BuildDate are injected at build time via -ldflags -X.
// Zero values are valid — the binary runs normally without them.
package main

import (
	"errors"
	"fmt"
	"os"
)

// Version is the release tag, injected via -ldflags -X main.Version=<value>.
var Version string

// Commit is the git short hash, injected via -ldflags -X main.Commit=<value>.
var Commit string

// BuildDate is the ISO-8601 build timestamp, injected via
// -ldflags -X main.BuildDate=<value>.
var BuildDate string

// exitError carries a specific exit code through Cobra's error return path.
// Commands return this instead of calling os.Exit directly, so that deferred
// cleanup runs and main resolves the code in one place.
type exitError struct {
	code int
}

func (e *exitError) Error() string { return "" }

func main() {
	if err := rootCmd.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(127)
	}
}
