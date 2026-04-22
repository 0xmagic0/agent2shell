package transfer

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

// ExecFunc executes a shell command on the target. Commands create this by
// closing over client.Run + socketPath.
type ExecFunc func(ctx context.Context, command string, timeout int) (*types.ExecResponse, error)

// ProgressFunc reports transfer progress. Called after each successful chunk.
type ProgressFunc func(transferred, total int64)

// DefaultChunkSize is 48KB of raw bytes per chunk (~64KB base64 encoded).
const DefaultChunkSize = 48 * 1024

var (
	ErrNoDecoder        = errors.New("no base64 decoder available on target")
	ErrNoChecksummer    = errors.New("no checksum tool available on target")
	ErrChecksumMismatch = errors.New("checksum mismatch")
	ErrEmptyFile        = errors.New("remote file is empty or does not exist")
)

// PushOpts configures a file push operation.
type PushOpts struct {
	Decoder     *Decoder
	Checksummer *Checksummer
	OnProgress  ProgressFunc
	ChunkSize   int // default DefaultChunkSize
	Timeout     int // per-chunk timeout in seconds; default 300
}

// PullOpts configures a file pull operation.
type PullOpts struct {
	Checksummer *Checksummer
	OnProgress  ProgressFunc
	ChunkSize   int
	Timeout     int
}

// verifyChecksum computes remote file MD5 and compares with localHash.
func verifyChecksum(ctx context.Context, exec ExecFunc, checksummer *Checksummer, remotePath string, localHash string, timeout int) error {
	if checksummer == nil {
		return ErrNoChecksummer
	}

	var cmd string
	quoted := shellQuote(remotePath)
	switch checksummer.Name {
	case "md5sum":
		cmd = fmt.Sprintf("md5sum %s | awk '{print $1}'", quoted)
	case "md5":
		cmd = fmt.Sprintf("md5 -q %s", quoted)
	default:
		return fmt.Errorf("transfer: unknown checksummer: %s", checksummer.Name)
	}

	resp, err := exec(ctx, cmd, timeout)
	if err != nil {
		return fmt.Errorf("transfer: checksum: %w", err)
	}
	if resp.ExitCode != 0 {
		return fmt.Errorf("transfer: checksum: exit %d: %s", resp.ExitCode, resp.Output)
	}

	remoteHash := strings.TrimSpace(resp.Output)
	if remoteHash != localHash {
		return fmt.Errorf("%w: local=%s remote=%s", ErrChecksumMismatch, localHash, remoteHash)
	}
	return nil
}

// computeMD5 returns the hex-encoded MD5 hash of data from reader.
func computeMD5(r io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("transfer: compute md5: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
