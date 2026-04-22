package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/spf13/cobra"
)

var (
	ErrNoSocket        = errors.New("no active sessions found")
	ErrMultipleSockets = errors.New("multiple sessions active")
)

var discoverFunc = socket.DiscoverSocket

// resolveSocket returns the Unix socket path to use for the command.
// It checks the -s/--socket flag first; if empty, it calls discoverFunc to
// auto-detect. Returns ErrNoSocket when none are found, or a wrapped
// ErrMultipleSockets when more than one is found (prompting the user to
// specify via -s).
func resolveSocket(cmd *cobra.Command) (string, error) {
	s, _ := cmd.Root().PersistentFlags().GetString("socket") // flag registered in root.go init()
	if s != "" {
		return s, nil
	}

	paths, err := discoverFunc()
	if err != nil {
		return "", fmt.Errorf("resolve socket: %w", err)
	}

	switch len(paths) {
	case 0:
		return "", ErrNoSocket
	case 1:
		return paths[0], nil
	default:
		return "", fmt.Errorf("%w, use -s to specify socket (found: %s)",
			ErrMultipleSockets, strings.Join(paths, ", "))
	}
}
