package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/listener"
	"github.com/0xmagic0/agent2shell/pkg/recorder"
	"github.com/0xmagic0/agent2shell/pkg/session"
	"github.com/0xmagic0/agent2shell/pkg/types"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var catchCmd = &cobra.Command{
	Use:   "catch",
	Short: "Catch a reverse shell connection",
	Args:  cobra.NoArgs,
	RunE:  runCatch,
}

func init() {
	catchCmd.Flags().IntP("port", "p", 4444, "TCP port to listen on")
	catchCmd.Flags().StringP("host", "H", "0.0.0.0", "TCP address to bind")
	catchCmd.Flags().DurationP("timeout", "t", 30*time.Second, "per-command execution timeout")
	catchCmd.Flags().String("tag", "", "optional session label")
	catchCmd.Flags().String("log", "", "JSONL log file for exec recording")
	rootCmd.AddCommand(catchCmd)
}

// buildCatchConfig reads flags from cmd and returns a listener.Config.
// Extracted for testability — tests call this directly without touching TCP.
// OnOutput and OnSessionReady are NOT set here; runCatch wires them after.
func buildCatchConfig(cmd *cobra.Command) (listener.Config, error) {
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return listener.Config{}, fmt.Errorf("catch: read port flag: %w", err)
	}

	host, err := cmd.Flags().GetString("host")
	if err != nil {
		return listener.Config{}, fmt.Errorf("catch: read host flag: %w", err)
	}

	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return listener.Config{}, fmt.Errorf("catch: read timeout flag: %w", err)
	}

	tag, err := cmd.Flags().GetString("tag")
	if err != nil {
		return listener.Config{}, fmt.Errorf("catch: read tag flag: %w", err)
	}

	cfg := listener.Config{
		Host:           host,
		Port:           port,
		DefaultTimeout: timeout,
		Tag:            tag,
		OnStatus: func(msg string) {
			fmt.Fprintf(os.Stderr, "[*] %s\n", msg)
		},
	}

	return cfg, nil
}

// interruptWindow is the maximum duration between two Ctrl-C presses to
// trigger a graceful shutdown. Configurable for testing.
var interruptWindow = 2 * time.Second

// handleInterrupt processes a single Ctrl-C event. It returns true when the
// caller should initiate a graceful shutdown (double-tap or no active session),
// and false when the Ctrl-C was forwarded to the remote shell as 0x03.
func handleInterrupt(sessRef *atomic.Pointer[session.Session], lastInterrupt *time.Time) bool {
	now := time.Now()
	if now.Sub(*lastInterrupt) < interruptWindow {
		// Double-tap within the window — shut down.
		return true
	}
	*lastInterrupt = now

	s := sessRef.Load()
	if s == nil {
		return true
	}
	// best-effort: session may close between Load and WriteRaw
	_ = s.WriteRaw([]byte{0x03})
	return false
}

func runCatch(cmd *cobra.Command, _ []string) error {
	cfg, err := buildCatchConfig(cmd)
	if err != nil {
		return err
	}

	logPath, err := cmd.Flags().GetString("log")
	if err != nil {
		return fmt.Errorf("catch: read log flag: %w", err)
	}
	if logPath != "" {
		rec, err := recorder.New(logPath)
		if err != nil {
			return fmt.Errorf("catch: open log: %w", err)
		}
		defer rec.Close() //nolint:errcheck // best-effort on shutdown
		cfg.Recorder = rec
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sessRef atomic.Pointer[session.Session]

	// lastInterrupt lives at runCatch scope so both the signal goroutine (line
	// mode) and the stdin read loop (raw mode) can share the double-tap state.
	// Only one of the two paths is active at a time, so no concurrent access.
	var lastInterrupt time.Time

	sigCh := make(chan os.Signal, 1)
	// In raw mode the terminal driver does not synthesise SIGINT from Ctrl-C,
	// so we only need SIGTERM here. We still subscribe to os.Interrupt so that
	// line-mode (piped stdin) keeps working unchanged.
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-sigCh:
				if sig == syscall.SIGTERM {
					cancel()
					return
				}
				// SIGINT — only fires in line mode (non-terminal stdin).
				if handleInterrupt(&sessRef, &lastInterrupt) {
					cancel()
					return
				}
				fmt.Fprintf(os.Stderr, "\n[*] Ctrl-C sent to remote (press again within 2s to quit)\n")
			}
		}
	}()

	cfg.OnOutput = func(line string) {
		fmt.Fprintln(os.Stdout, line)
	}

	cfg.OnExec = func(command string, resp *types.ExecResponse) {
		fmt.Fprintf(os.Stderr, "[agent] %s\n", command)
		if resp.Output != "" {
			fmt.Fprint(os.Stdout, resp.Output)
			if !strings.HasSuffix(resp.Output, "\n") {
				fmt.Fprintln(os.Stdout)
			}
		}
	}

	// Terminal state is managed at runCatch scope so Restore always runs
	// when Listen returns — even if the stdin goroutine is still blocked
	// on os.Stdin.Read().
	stdinFd := int(os.Stdin.Fd())
	var termState *term.State

	cfg.OnSessionReady = func(ctx context.Context, sess *session.Session, socketPath string) {
		sessRef.Store(sess)

		if term.IsTerminal(stdinFd) {
			oldState, err := term.MakeRaw(stdinFd)
			if err == nil {
				termState = oldState
				runRawStdin(ctx, sess, &sessRef, &lastInterrupt, cancel)
				return
			}
			// best-effort: MakeRaw failed — fall through to line mode
		}

		runLineStdin(ctx, sess)
	}

	l, err := listener.New(cfg)
	if err != nil {
		return fmt.Errorf("catch: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[*] Listening on %s:%d...\n", cfg.Host, cfg.Port)

	if err := l.Listen(ctx); err != nil {
		if termState != nil {
			// best-effort: restore terminal before returning error
			_ = term.Restore(stdinFd, termState)
		}
		return fmt.Errorf("catch: %w", err)
	}

	if termState != nil {
		// best-effort: restore terminal on clean shutdown
		_ = term.Restore(stdinFd, termState)
	}

	fmt.Fprintf(os.Stderr, "[*] Session closed.\n")

	return nil
}

// runRawStdin reads stdin byte-by-byte in raw terminal mode and forwards bytes
// to the remote shell. Ctrl-C (0x03) is handled via the double-tap logic
// rather than relying on SIGINT (which raw mode suppresses).
func runRawStdin(
	ctx context.Context,
	sess *session.Session,
	sessRef *atomic.Pointer[session.Session],
	lastInterrupt *time.Time,
	cancel context.CancelFunc,
) {
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}

		// Scan for Ctrl-C (0x03) and handle the double-tap logic.
		// We process the buffer in one pass, collecting non-0x03 bytes and
		// flushing them before handling each interrupt.
		out := buf[:0]
		for i := 0; i < n; i++ {
			if buf[i] != 0x03 {
				out = append(out, buf[i])
				continue
			}
			// Flush any bytes accumulated before this 0x03.
			if len(out) > 0 {
				if err := sess.WriteRaw(out); err != nil {
					return
				}
				out = out[:0]
			}
			if handleInterrupt(sessRef, lastInterrupt) {
				cancel()
				return
			}
			// \r\n because raw mode does not translate \n to \r\n.
			fmt.Fprintf(os.Stderr, "\r\n[*] Ctrl-C sent to remote (press again within 2s to quit)\r\n")
		}

		if len(out) > 0 {
			if err := sess.WriteRaw(out); err != nil {
				return
			}
		}
	}
}

// runLineStdin reads stdin line-by-line (bufio.Scanner) and forwards each
// line to the remote shell. Used when stdin is not a terminal (piped input)
// or when MakeRaw fails.
func runLineStdin(ctx context.Context, sess *session.Session) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := sess.WriteRaw([]byte(scanner.Text() + "\n")); err != nil {
			return
		}
	}
	// best-effort: stdin read errors (scanner.Err) are non-actionable here —
	// the goroutine is exiting anyway and the session is shutting down
}
