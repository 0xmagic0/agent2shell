package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCatchTestCmd returns a fresh Cobra command with the same flags as
// catchCmd. Used in tests so we can set flag values without mutating the
// package-level command registered with rootCmd.
func newCatchTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catch"}
	cmd.Flags().IntP("port", "p", 4444, "TCP port to listen on")
	cmd.Flags().StringP("host", "H", "0.0.0.0", "TCP address to bind")
	cmd.Flags().DurationP("timeout", "t", 30*time.Second, "per-command execution timeout")
	cmd.Flags().String("tag", "", "optional session label")
	return cmd
}

func TestBuildCatchConfig_Defaults(t *testing.T) {
	cmd := newCatchTestCmd()

	cfg, err := buildCatchConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, 4444, cfg.Port)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 30*time.Second, cfg.DefaultTimeout)
	assert.Equal(t, "", cfg.Tag)
}

func TestBuildCatchConfig_CustomFlags(t *testing.T) {
	cmd := newCatchTestCmd()
	require.NoError(t, cmd.Flags().Set("port", "9001"))
	require.NoError(t, cmd.Flags().Set("host", "127.0.0.1"))
	require.NoError(t, cmd.Flags().Set("timeout", "1m"))
	require.NoError(t, cmd.Flags().Set("tag", "my-session"))

	cfg, err := buildCatchConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, 9001, cfg.Port)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, time.Minute, cfg.DefaultTimeout)
	assert.Equal(t, "my-session", cfg.Tag)
}

func TestBuildCatchConfig_OnStatus(t *testing.T) {
	cmd := newCatchTestCmd()

	cfg, err := buildCatchConfig(cmd)
	require.NoError(t, err)

	assert.NotNil(t, cfg.OnStatus, "OnStatus callback must be set")
}
