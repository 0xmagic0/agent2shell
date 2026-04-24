package main

import (
	"testing"
)

// TestRunCmd_NoStreamFlagExists verifies that the --no-stream flag is
// registered on the run command.
func TestRunCmd_NoStreamFlagExists(t *testing.T) {
	f := runCmd.Flags().Lookup("no-stream")
	if f == nil {
		t.Fatal("--no-stream flag not registered on run command")
	}
	if f.Value.Type() != "bool" {
		t.Errorf("--no-stream flag type = %q, want bool", f.Value.Type())
	}
}

// TestRunCmd_NoStreamFlagDefault verifies that --no-stream defaults to false
// (streaming is the default behavior).
func TestRunCmd_NoStreamFlagDefault(t *testing.T) {
	f := runCmd.Flags().Lookup("no-stream")
	if f == nil {
		t.Fatal("--no-stream flag not registered")
	}
	if f.DefValue != "false" {
		t.Errorf("--no-stream default = %q, want false", f.DefValue)
	}
}
