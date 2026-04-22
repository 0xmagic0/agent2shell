package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xmagic0/agent2shell/pkg/listener"
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
	rootCmd.AddCommand(catchCmd)
}

// buildCatchConfig reads flags from cmd and returns a listener.Config.
// Extracted for testability — tests call this directly without touching TCP.
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

func runCatch(cmd *cobra.Command, _ []string) error {
	cfg, err := buildCatchConfig(cmd)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
