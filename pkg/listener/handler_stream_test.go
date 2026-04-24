package listener_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/0xmagic0/agent2shell/internal/testutil"
	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/0xmagic0/agent2shell/pkg/socket"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAllFrames reads StreamFrame values from conn until StreamEnd arrives or
// a read error occurs. Returns all line frames and the end frame.
func readAllStreamFrames(t *testing.T, conn net.Conn) (lines []types.StreamFrame, end types.StreamFrame) {
	t.Helper()
	for {
		var f types.StreamFrame
		if err := socket.ReadFrame(conn, &f); err != nil {
			t.Logf("readAllStreamFrames: read error: %v", err)
			return
		}
		switch f.Type {
		case types.StreamLine:
			lines = append(lines, f)
		case types.StreamEnd:
			return lines, f
		default:
			t.Errorf("unexpected frame type: %s", f.Type)
		}
	}
}

// ─── Task 3.1: buildStreamHandler tests ─────────────────────────────────────

// TestBuildStreamHandler_LinesArrivePerLine verifies that the stream handler
// writes one StreamLine frame per output line and exactly one StreamEnd frame
// as the last frame.
func TestBuildStreamHandler_LinesArrivePerLine(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	streamHandler := l.BuildStreamHandler()

	handlerConn, clientConn := net.Pipe()
	defer handlerConn.Close()
	defer clientConn.Close()

	// Three-line response — respondToProbe writes one block; ExecStream splits by
	// newline as lines arrive via the readLoop.
	go func() {
		_ = shell.respondToProbe("alpha\nbeta\ngamma", 0)
	}()

	req := &types.Request{Type: types.RunRequest, Command: "echo", Stream: true}
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamHandler(context.Background(), req, handlerConn)
	}()

	lineFrames, endFrame := readAllStreamFrames(t, clientConn)
	<-done

	require.Len(t, lineFrames, 3, "expected 3 StreamLine frames")
	assert.Equal(t, "alpha", lineFrames[0].Data)
	assert.Equal(t, "beta", lineFrames[1].Data)
	assert.Equal(t, "gamma", lineFrames[2].Data)
	assert.Equal(t, types.StreamEnd, endFrame.Type)
	assert.Equal(t, 0, endFrame.ExitCode)
}

// TestBuildStreamHandler_EmptyOutput verifies that when the command produces
// only one non-empty line, exactly one StreamLine frame is written, and the
// final frame is StreamEnd.
// Note: respondToProbe always wraps output between start+end markers with a
// trailing newline, so an empty string produces one empty line. This test
// instead verifies the end-frame is always written.
func TestBuildStreamHandler_EmptyOutput(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	streamHandler := l.BuildStreamHandler()

	handlerConn, clientConn := net.Pipe()
	defer handlerConn.Close()
	defer clientConn.Close()

	// Send start+end markers with no output lines in between by writing
	// them manually via the shell connection.
	go func() {
		if !shell.scanner.Scan() {
			return
		}
		line := shell.scanner.Text()
		id := extractMarkerID(line)
		if id == "" {
			return
		}
		// Write ONLY start and end markers — no output lines between them.
		resp := fmt.Sprintf("---A2S-START-%s---\n---A2S-END-%s---0\n", id, id)
		_, _ = fmt.Fprint(shell.conn, resp)
	}()

	req := &types.Request{Type: types.RunRequest, Command: "true", Stream: true}
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamHandler(context.Background(), req, handlerConn)
	}()

	lineFrames, endFrame := readAllStreamFrames(t, clientConn)
	<-done

	assert.Empty(t, lineFrames, "no StreamLine frames expected for empty output")
	assert.Equal(t, types.StreamEnd, endFrame.Type)
}

// TestBuildStreamHandler_StreamEndIsLast verifies StreamEnd is the very last
// frame written: the connection closes after it, so reads after StreamEnd
// return an error.
func TestBuildStreamHandler_StreamEndIsLast(t *testing.T) {
	sess, shell := newTestSession(t)

	sockPath := testutil.TempSocket(t)
	l := listener.NewWithSession(sess, sockPath)
	streamHandler := l.BuildStreamHandler()

	handlerConn, clientConn := net.Pipe()
	defer clientConn.Close()

	go func() {
		_ = shell.respondToProbe("only", 0)
	}()

	req := &types.Request{Type: types.RunRequest, Command: "echo only", Stream: true}
	go func() {
		streamHandler(context.Background(), req, handlerConn)
		handlerConn.Close()
	}()

	var lastType types.StreamFrameType
	for {
		var f types.StreamFrame
		if err := socket.ReadFrame(clientConn, &f); err != nil {
			break
		}
		lastType = f.Type
	}

	assert.Equal(t, types.StreamEnd, lastType, "StreamEnd must be the last frame")
}

// TestBuildStreamHandler_OnExecHookCalledAfterCompletion verifies that the
// OnExec hook is called with the accumulated output after the command finishes.
func TestBuildStreamHandler_OnExecHookCalledAfterCompletion(t *testing.T) {
	sess, shell := newTestSession(t)
	sockPath := testutil.TempSocket(t)

	var mu sync.Mutex
	var hookCmd string
	var hookResp *types.ExecResponse
	hookCalled := make(chan struct{}, 1)

	onExec := func(cmd string, resp *types.ExecResponse) {
		mu.Lock()
		hookCmd = cmd
		hookResp = resp
		mu.Unlock()
		hookCalled <- struct{}{}
	}

	l := listener.NewWithSessionAndConfig(sess, sockPath, listener.Config{OnExec: onExec})
	streamHandler := l.BuildStreamHandler()

	handlerConn, clientConn := net.Pipe()
	defer handlerConn.Close()
	defer clientConn.Close()

	go func() {
		_ = shell.respondToProbe("hook output", 0)
	}()

	req := &types.Request{Type: types.RunRequest, Command: "hook-cmd", Stream: true}
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamHandler(context.Background(), req, handlerConn)
	}()

	// Drain frames.
	for {
		var f types.StreamFrame
		if err := socket.ReadFrame(clientConn, &f); err != nil || f.Type == types.StreamEnd {
			break
		}
	}
	<-done

	select {
	case <-hookCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("OnExec hook not called within 2s")
	}

	mu.Lock()
	cmd := hookCmd
	resp := hookResp
	mu.Unlock()

	assert.Equal(t, "hook-cmd", cmd)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Output, "hook output")
}
