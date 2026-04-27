package transfer

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/0xmagic0/agent2shell/pkg/types"
)

// smallFileThreshold is the maximum file size handled via a single base64 read (16 MB).
const smallFileThreshold = 16 * 1024 * 1024

// Pull downloads a remote file to localPath.
// It uses wc -c to detect file size, then selects a small-file (single base64)
// or large-file (dd-chunked) path. Temp file is always in the same directory as
// localPath so os.Rename is atomic.
// Returns ErrNoEncoder if opts.Encoder is nil. Checksummer is optional; nil skips verification.
func Pull(ctx context.Context, exec ExecFunc, remotePath, localPath string, opts PullOpts) error {
	if opts.Encoder == nil {
		return ErrNoEncoder
	}
	if opts.ChunkSize == 0 {
		opts.ChunkSize = DefaultChunkSize
	}
	if opts.Timeout == 0 {
		opts.Timeout = 300
	}

	// 1. Get remote file size.
	size, err := remoteFileSize(ctx, exec, remotePath, opts.Timeout)
	if err != nil {
		return err
	}
	if size == 0 {
		return ErrEmptyFile
	}

	// 2. Create temp file in same directory as localPath (required for atomic rename).
	dir := filepath.Dir(localPath)
	tmp, err := os.CreateTemp(dir, ".a2s-pull-*")
	if err != nil {
		return fmt.Errorf("transfer: pull: create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Cleanup on error: removed by deferred function unless success flag is set.
	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	// 3. Transfer content.
	if size <= smallFileThreshold {
		if err := pullSmall(ctx, exec, remotePath, tmp, size, opts); err != nil {
			return err
		}
	} else {
		if err := pullChunked(ctx, exec, remotePath, tmp, size, opts); err != nil {
			return err
		}
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("transfer: pull: close temp file: %w", err)
	}

	// 4. Atomic rename.
	if err := os.Rename(tmpPath, localPath); err != nil {
		return fmt.Errorf("transfer: pull: rename: %w", err)
	}

	// 5. Checksum verification (file is at localPath after rename).
	// verifyChecksum is a no-op when opts.Checksummer is nil.
	if opts.Checksummer != nil {
		algo := opts.Checksummer.HashAlgo
		if algo == "" {
			algo = "md5"
		}
		local, err := localHash(localPath, algo)
		if err != nil {
			return err
		}
		if err := verifyChecksum(ctx, exec, opts.Checksummer, remotePath, local, opts.Timeout); err != nil {
			// File already written — caller decides cleanup.
			return err
		}
	}

	success = true
	return nil
}

// remoteFileSize executes `wc -c < remotePath` and returns the byte count.
// Returns 0 and ErrEmptyFile if the remote file is empty.
// Returns an error if the command fails or the output cannot be parsed.
func remoteFileSize(ctx context.Context, exec ExecFunc, remotePath string, timeout int) (int64, error) {
	cmd := fmt.Sprintf("wc -c < %s", shellQuote(remotePath))
	resp, err := execOrContextErr(ctx, exec, cmd, timeout)
	if err != nil {
		return 0, fmt.Errorf("transfer: pull: get file size: %w", err)
	}
	if resp.ExitCode != 0 {
		return 0, fmt.Errorf("transfer: pull: get file size: exit %d: %s", resp.ExitCode, resp.Output)
	}

	raw := strings.TrimSpace(resp.Output)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, ErrEmptyFile
	}
	return n, nil
}

// pullSmall transfers a small file (≤ 16 MB) via a single encoder command.
func pullSmall(ctx context.Context, exec ExecFunc, remotePath string, out *os.File, size int64, opts PullOpts) error {
	cmd := fmt.Sprintf("cat %s | %s", shellQuote(remotePath), opts.Encoder.Command)
	resp, err := execOrContextErr(ctx, exec, cmd, opts.Timeout)
	if err != nil {
		return fmt.Errorf("transfer: pull: encode: %w", err)
	}
	if resp.ExitCode != 0 {
		return fmt.Errorf("transfer: pull: encode: exit %d: %s", resp.ExitCode, resp.Output)
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(resp.Output))
	if err != nil {
		return fmt.Errorf("transfer: pull: decode base64: %w", err)
	}

	if _, err := out.Write(data); err != nil {
		return fmt.Errorf("transfer: pull: write: %w", err)
	}

	if opts.OnProgress != nil {
		opts.OnProgress(size, size)
	}
	return nil
}

// pullChunked transfers a large file using dd chunks, each decoded from base64.
func pullChunked(ctx context.Context, exec ExecFunc, remotePath string, out *os.File, size int64, opts PullOpts) error {
	chunkSize := int64(opts.ChunkSize)
	numChunks := int((size + chunkSize - 1) / chunkSize)

	for i := 0; i < numChunks; i++ {
		if ctx.Err() != nil {
			return fmt.Errorf("transfer: pull: %w", ctx.Err())
		}

		offset := int64(i) * chunkSize
		count := chunkSize
		if offset+count > size {
			count = size - offset
		}

		cmd := fmt.Sprintf(
			"dd if=%s bs=1 skip=%d count=%d 2>/dev/null | %s",
			shellQuote(remotePath), offset, count, opts.Encoder.Command,
		)
		resp, err := execOrContextErr(ctx, exec, cmd, opts.Timeout)
		if err != nil {
			return fmt.Errorf("transfer: pull: chunk %d: %w", i, err)
		}
		if resp.ExitCode != 0 {
			return fmt.Errorf("transfer: pull: chunk %d: exit %d: %s", i, resp.ExitCode, resp.Output)
		}

		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(resp.Output))
		if err != nil {
			return fmt.Errorf("transfer: pull: chunk %d: decode: %w", i, err)
		}

		if _, err := out.Write(data); err != nil {
			return fmt.Errorf("transfer: pull: chunk %d: write: %w", i, err)
		}

		transferred := offset + count
		if opts.OnProgress != nil {
			opts.OnProgress(transferred, size)
		}
	}
	return nil
}

// localHash computes the hash of the file at path using the named algorithm ("md5" or "sha256").
func localHash(path string, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("transfer: pull: open for checksum: %w", err)
	}
	defer f.Close()
	return computeHash(f, algo)
}

// execOrContextErr calls exec and wraps context errors for clean propagation.
func execOrContextErr(ctx context.Context, exec ExecFunc, cmd string, timeout int) (*types.ExecResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	resp, err := exec(ctx, cmd, timeout)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return resp, nil
}
