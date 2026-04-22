package main

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.PersistentFlags().StringP("socket", "s", "", "")
	return cmd
}

func TestResolveSocket(t *testing.T) {
	errFail := errors.New("discover failed")

	tests := []struct {
		name         string
		socketFlag   string
		discoverRet  []string
		discoverErr  error
		wantPath     string
		wantErr      error
		wantErrWraps error
	}{
		{
			name:       "flag provided",
			socketFlag: "/tmp/a2s-2.sock",
			wantPath:   "/tmp/a2s-2.sock",
			wantErr:    nil,
		},
		{
			name:        "one socket",
			socketFlag:  "",
			discoverRet: []string{"/tmp/a2s-1.sock"},
			wantPath:    "/tmp/a2s-1.sock",
			wantErr:     nil,
		},
		{
			name:        "no sockets",
			socketFlag:  "",
			discoverRet: []string{},
			wantPath:    "",
			wantErr:     ErrNoSocket,
		},
		{
			name:         "multiple sockets",
			socketFlag:   "",
			discoverRet:  []string{"/tmp/a2s-1.sock", "/tmp/a2s-2.sock"},
			wantPath:     "",
			wantErrWraps: ErrMultipleSockets,
		},
		{
			name:         "discover error",
			socketFlag:   "",
			discoverRet:  nil,
			discoverErr:  errFail,
			wantPath:     "",
			wantErrWraps: errFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Swap discoverFunc.
			orig := discoverFunc
			called := false
			discoverFunc = func() ([]string, error) {
				called = true
				return tt.discoverRet, tt.discoverErr
			}
			t.Cleanup(func() { discoverFunc = orig })

			cmd := newTestCmd()
			if tt.socketFlag != "" {
				require.NoError(t, cmd.PersistentFlags().Set("socket", tt.socketFlag))
			}

			got, err := resolveSocket(cmd)

			assert.Equal(t, tt.wantPath, got)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else if tt.wantErrWraps != nil {
				assert.ErrorIs(t, err, tt.wantErrWraps)
			} else {
				require.NoError(t, err)
			}

			// When flag is provided, discoverFunc must NOT be called.
			if tt.socketFlag != "" {
				assert.False(t, called, "discoverFunc should not be called when -s flag is set")
			}
		})
	}
}
