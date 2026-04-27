package transfer

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
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
	ErrNoEncoder        = errors.New("no base64 encoder available on target")
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
	Encoder     *Encoder     // required — nil returns ErrNoEncoder
	Checksummer *Checksummer // optional — nil skips checksum verification
	OnProgress  ProgressFunc
	ChunkSize   int
	Timeout     int
}

// verifyChecksum runs the remote checksum command built from checksummer.VerifyTemplate
// and compares the result with localHash. Returns nil when checksummer is nil (skip).
func verifyChecksum(ctx context.Context, exec ExecFunc, checksummer *Checksummer, remotePath string, localHash string, timeout int) error {
	if checksummer == nil {
		return nil // optional — skip verification silently
	}

	cmd := fmt.Sprintf(checksummer.VerifyTemplate, shellQuote(remotePath))

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

// computeHash returns the hex-encoded hash of data from reader using the named algorithm.
// Supported algo values: "md5", "sha256".
func computeHash(r io.Reader, algo string) (string, error) {
	var h hash.Hash
	switch algo {
	case "md5":
		h = md5.New()
	case "sha256":
		h = sha256.New()
	default:
		return "", fmt.Errorf("transfer: compute hash: unsupported algo %q", algo)
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("transfer: compute hash: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// computeMD5 returns the hex-encoded MD5 hash of data from reader.
// Kept for callers that have not migrated to computeHash.
func computeMD5(r io.Reader) (string, error) {
	return computeHash(r, "md5")
}
