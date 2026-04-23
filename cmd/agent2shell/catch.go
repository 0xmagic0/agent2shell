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
	catchCmd.Flags().Bool("auto-upgrade", false, "attempt shell upgrade on connect")
	rootCmd.AddCommand(catchCmd)
}

// buildCatchConfig reads flags from cmd and returns a listener.Config.
// Extracted for testability — tests call this directly without touching TCP.
// OnOutput, OnExec, and OnSessionReady are NOT set here; runCatch wires them.
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

	autoUpgrade, err := cmd.Flags().GetBool("auto-upgrade")
	if err != nil {
		return listener.Config{}, fmt.Errorf("catch: read auto-upgrade flag: %w", err)
	}

	cfg := listener.Config{
		Host:           host,
		Port:           port,
		DefaultTimeout: timeout,
		Tag:            tag,
		AutoUpgrade:    autoUpgrade,
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

	// lastInterrupt tracks the last Ctrl-C for double-tap detection.
	// Only accessed from the signal goroutine — no concurrent access.
	var lastInterrupt time.Time

	sigCh := make(chan os.Signal, 1)
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
				if handleInterrupt(&sessRef, &lastInterrupt) {
					cancel()
					return
				}
				fmt.Fprintf(os.Stderr, "\n[*] Ctrl-C sent to remote (press again within 2s to quit)\n")
			}
		}
	}()

	// lastSent tracks the most recent operator-typed line so we can
	// suppress the remote shell's echo of it (prompt + command).
	var lastSent atomic.Value

	cfg.OnOutput = func(line string) {
		if sent, ok := lastSent.Load().(string); ok && sent != "" {
			if strings.HasSuffix(line, sent) {
				lastSent.Store("")
				return
			}
		}
		fmt.Fprintln(os.Stdout, line)
	}

	cfg.OnExec = func(command string, resp *types.ExecResponse) {
		fmt.Fprintf(os.Stderr, "[a2s] %s\n", command)
		if resp.Output != "" {
			fmt.Fprint(os.Stdout, resp.Output)
			if !strings.HasSuffix(resp.Output, "\n") {
				fmt.Fprintln(os.Stdout)
			}
		}
	}

	cfg.OnSessionReady = func(ctx context.Context, sess *session.Session, socketPath string) {
		sessRef.Store(sess)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line := scanner.Text()
			lastSent.Store(line)
			if err := sess.WriteRaw([]byte(line + "\n")); err != nil {
				return
			}
		}
		// best-effort: stdin read errors (scanner.Err) are non-actionable here —
		// the goroutine is exiting anyway and the session is shutting down
	}

	l, err := listener.New(cfg)
	if err != nil {
		return fmt.Errorf("catch: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[*] Listening on %s:%d...\n", cfg.Host, cfg.Port)

	if err := l.Listen(ctx); err != nil {
		return fmt.Errorf("catch: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[*] Session closed.\n")

	return nil
}
