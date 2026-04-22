package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

func fixedTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestFormatStatus_AllFields(t *testing.T) {
	connectedAt := fixedTime("2024-01-15T10:30:00Z")
	info := &types.SessionInfo{
		RemoteAddr:       "10.0.0.5:54321",
		Shell:            "/bin/bash",
		User:             "www-data",
		Hostname:         "web-01",
		OS:               "linux",
		Arch:             "amd64",
		Distro:           "Ubuntu 22.04",
		ConnectedAt:      connectedAt,
		CommandsExecuted: 42,
		Tag:              "webserver",
		Recording:        false,
	}

	var buf bytes.Buffer
	formatStatus(&buf, info)
	out := buf.String()

	checks := []struct {
		label string
		value string
	}{
		{"Remote label", "Remote:"},
		{"Remote value", "10.0.0.5:54321"},
		{"Shell label", "Shell:"},
		{"Shell value", "/bin/bash"},
		{"User label", "User:"},
		{"User value", "www-data"},
		{"Hostname label", "Hostname:"},
		{"Hostname value", "web-01"},
		{"OS label", "OS:"},
		{"OS value", "linux"},
		{"Arch label", "Arch:"},
		{"Arch value", "amd64"},
		{"Distro label", "Distro:"},
		{"Distro value", "Ubuntu 22.04"},
		{"Connected label", "Connected:"},
		{"Connected RFC3339", "2024-01-15T10:30:00Z"},
		{"Connected ago", "ago"},
		{"Commands label", "Commands:"},
		{"Commands value", "42"},
		{"Tag label", "Tag:"},
		{"Tag value", "webserver"},
		{"Recording label", "Recording:"},
		{"Recording value", "no"},
	}

	for _, c := range checks {
		t.Run(c.label, func(t *testing.T) {
			if !strings.Contains(out, c.value) {
				t.Errorf("output missing %q\nfull output:\n%s", c.value, out)
			}
		})
	}
}

func TestFormatStatus_EmptyDistro(t *testing.T) {
	info := &types.SessionInfo{
		RemoteAddr:  "1.2.3.4:1234",
		Shell:       "/bin/sh",
		User:        "root",
		Hostname:    "box",
		OS:          "linux",
		Arch:        "arm64",
		Distro:      "", // empty — line must be omitted
		ConnectedAt: fixedTime("2024-06-01T00:00:00Z"),
	}

	var buf bytes.Buffer
	formatStatus(&buf, info)
	out := buf.String()

	if strings.Contains(out, "Distro:") {
		t.Errorf("expected no Distro line when Distro is empty, got:\n%s", out)
	}
}

func TestFormatStatus_EmptyTag(t *testing.T) {
	info := &types.SessionInfo{
		RemoteAddr:  "1.2.3.4:1234",
		Shell:       "/bin/sh",
		User:        "root",
		Hostname:    "box",
		OS:          "linux",
		Arch:        "arm64",
		ConnectedAt: fixedTime("2024-06-01T00:00:00Z"),
		Tag:         "", // empty — line must be omitted
	}

	var buf bytes.Buffer
	formatStatus(&buf, info)
	out := buf.String()

	if strings.Contains(out, "Tag:") {
		t.Errorf("expected no Tag line when Tag is empty, got:\n%s", out)
	}
}

func TestFormatStatus_Recording(t *testing.T) {
	base := &types.SessionInfo{
		RemoteAddr:  "1.2.3.4:1234",
		Shell:       "/bin/sh",
		User:        "root",
		Hostname:    "box",
		OS:          "linux",
		Arch:        "arm64",
		ConnectedAt: fixedTime("2024-06-01T00:00:00Z"),
	}

	tests := []struct {
		name      string
		recording bool
		want      string
	}{
		{"recording true", true, "yes"},
		{"recording false", false, "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := *base
			info.Recording = tt.recording

			var buf bytes.Buffer
			formatStatus(&buf, &info)
			out := buf.String()

			if !strings.Contains(out, tt.want) {
				t.Errorf("expected %q in output for Recording=%v\nfull output:\n%s", tt.want, tt.recording, out)
			}
		})
	}
}

func TestFormatStatus_ConnectedAt(t *testing.T) {
	connectedAt := fixedTime("2024-03-10T08:00:00Z")
	info := &types.SessionInfo{
		RemoteAddr:  "5.5.5.5:9999",
		Shell:       "/bin/zsh",
		User:        "admin",
		Hostname:    "server",
		OS:          "darwin",
		Arch:        "amd64",
		ConnectedAt: connectedAt,
	}

	var buf bytes.Buffer
	formatStatus(&buf, info)
	out := buf.String()

	if !strings.Contains(out, "2024-03-10T08:00:00Z") {
		t.Errorf("output missing RFC3339 timestamp\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "ago") {
		t.Errorf("output missing 'ago' suffix on connected line\nfull output:\n%s", out)
	}
}
