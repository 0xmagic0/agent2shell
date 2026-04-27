package transfer

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"os"
)

// Push uploads a local file to the remote target via base64-encoded chunks.
// It reads localPath, encodes each chunk, and pipes it through the decoder
// command into remotePath using printf to avoid echo's trailing newline.
//
// Returns ErrNoDecoder if opts.Decoder is nil.
// Checksummer is optional: nil skips verification (no error returned).
func Push(ctx context.Context, exec ExecFunc, localPath, remotePath string, opts PushOpts) error {
	if opts.Decoder == nil {
		return ErrNoDecoder
	}

	if opts.ChunkSize == 0 {
		opts.ChunkSize = DefaultChunkSize
	}
	if opts.Timeout == 0 {
		opts.Timeout = 300
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("transfer: push open %s: %w", localPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("transfer: push stat %s: %w", localPath, err)
	}
	totalSize := info.Size()

	// Select hash algorithm based on checksummer (default md5 for backward compat).
	var localHasher hash.Hash
	hashAlgo := "md5"
	if opts.Checksummer != nil && opts.Checksummer.HashAlgo != "" {
		hashAlgo = opts.Checksummer.HashAlgo
	}
	localHasher, err = newHasher(hashAlgo)
	if err != nil {
		return err
	}

	buf := make([]byte, opts.ChunkSize)
	quoted := shellQuote(remotePath)
	chunkIdx := 0
	var written int64

	for {
		if ctx.Err() != nil {
			return fmt.Errorf("transfer: push: %w", ctx.Err())
		}

		n, readErr := io.ReadFull(f, buf)
		if n == 0 {
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return fmt.Errorf("transfer: push read chunk %d: %w", chunkIdx, readErr)
			}
		}

		chunk := buf[:n]
		localHasher.Write(chunk)

		encoded := base64.StdEncoding.EncodeToString(chunk)

		redirect := ">"
		if chunkIdx > 0 {
			redirect = ">>"
		}

		cmd := fmt.Sprintf("printf '%%s' '%s' | %s %s %s",
			encoded,
			opts.Decoder.Command,
			redirect,
			quoted,
		)

		resp, execErr := exec(ctx, cmd, opts.Timeout)
		if execErr != nil {
			totalChunks := totalChunkCount(totalSize, int64(opts.ChunkSize))
			return fmt.Errorf("transfer: push chunk %d/%d: %w", chunkIdx+1, totalChunks, execErr)
		}
		if resp.ExitCode != 0 {
			totalChunks := totalChunkCount(totalSize, int64(opts.ChunkSize))
			return fmt.Errorf("transfer: push chunk %d/%d: exit %d: %s",
				chunkIdx+1, totalChunks, resp.ExitCode, resp.Output)
		}

		written += int64(n)
		if opts.OnProgress != nil {
			opts.OnProgress(written, totalSize)
		}

		chunkIdx++

		if readErr == io.ErrUnexpectedEOF || readErr == io.EOF {
			break
		}
	}

	// Skip verification when no checksummer available.
	if opts.Checksummer == nil {
		return nil
	}

	localHex := fmt.Sprintf("%x", localHasher.Sum(nil))
	return verifyChecksum(ctx, exec, opts.Checksummer, remotePath, localHex, opts.Timeout)
}

// newHasher returns a hash.Hash for the named algorithm.
func newHasher(algo string) (hash.Hash, error) {
	switch algo {
	case "md5":
		return md5.New(), nil
	case "sha256":
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("transfer: push: unsupported hash algo %q", algo)
	}
}

// totalChunkCount computes the total number of chunks for human-readable error messages.
func totalChunkCount(fileSize, chunkSize int64) int64 {
	if fileSize == 0 {
		return 1
	}
	chunks := fileSize / chunkSize
	if fileSize%chunkSize != 0 {
		chunks++
	}
	return chunks
}
