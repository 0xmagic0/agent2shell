package socket

import (
	"fmt"
	"path/filepath"
	"sort"
)

// socketGlobPattern is the glob used to discover agent2shell sockets in /tmp.
const socketGlobPattern = "/tmp/a2s-*.sock"

// DiscoverSocket returns the sorted list of agent2shell Unix domain socket
// paths that currently exist in /tmp. An empty result is not an error — it
// simply means no sessions are running. Paths are returned in ascending
// lexicographic order.
func DiscoverSocket() ([]string, error) {
	matches, err := filepath.Glob(socketGlobPattern)
	if err != nil {
		return nil, fmt.Errorf("discover socket: glob %s: %w", socketGlobPattern, err)
	}

	if matches == nil {
		return []string{}, nil
	}

	sort.Strings(matches)
	return matches, nil
}

// NextSocketPath returns the path for the next available agent2shell socket.
// It finds the lowest integer N >= 1 such that /tmp/a2s-N.sock does not
// exist, and returns that path without creating any file.
func NextSocketPath() (string, error) {
	existing, err := DiscoverSocket()
	if err != nil {
		return "", fmt.Errorf("next socket path: %w", err)
	}

	// Build a set of existing paths for O(1) lookup.
	set := make(map[string]struct{}, len(existing))
	for _, p := range existing {
		set[p] = struct{}{}
	}

	for n := 1; ; n++ {
		candidate := fmt.Sprintf("/tmp/a2s-%d.sock", n)
		if _, taken := set[candidate]; !taken {
			return candidate, nil
		}
	}
}
