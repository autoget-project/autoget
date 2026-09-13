package organizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"time"

	"github.com/autoget-project/autoget/protocol"
)

var (
	// ErrParked is returned when an upload task is parked (suspended) due to retry exhaustion or file access error.
	// It is not considered a permanent failure; the orchestrator will retry on the next tick.
	ErrParked = errors.New("upload parked for retry on next tick")
)

// ProgressFunc is called after each chunk is successfully uploaded.
type ProgressFunc func(bytesUploaded, totalBytes int64)

// Uploader coordinates resumable file uploading with retries, backoff, and offset reconciliation.
type Uploader struct {
	client      *Client
	chunkSize   int64
	maxRetries  int
	backoffBase time.Duration
}

// UploaderOption configures an Uploader.
type UploaderOption func(*Uploader)

// WithChunkSize sets the chunk size in bytes.
func WithChunkSize(size int64) UploaderOption {
	return func(u *Uploader) {
		if size > 0 {
			u.chunkSize = size
		}
	}
}

// WithMaxRetries sets the maximum retry count for a chunk.
func WithMaxRetries(retries int) UploaderOption {
	return func(u *Uploader) {
		if retries >= 0 {
			u.maxRetries = retries
		}
	}
}

// WithBackoffBase sets the base backoff duration for retries.
func WithBackoffBase(base time.Duration) UploaderOption {
	return func(u *Uploader) {
		if base > 0 {
			u.backoffBase = base
		}
	}
}

// NewUploader constructs an Uploader.
func NewUploader(client *Client, opts ...UploaderOption) *Uploader {
	u := &Uploader{
		client:      client,
		chunkSize:   32 * 1024 * 1024, // 32MB default
		maxRetries:  5,
		backoffBase: 1 * time.Second,
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// UploadFile uploads a local source file to the organizer service according to the resumable upload protocol.
func (u *Uploader) UploadFile(ctx context.Context, srcPath string, req *UploadInitRequest, onProgress ProgressFunc) error {
	// 1. Inspect source file (O_RDONLY)
	fi, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("%w: stat source file failed: %v", ErrParked, err)
	}
	if fi.Size() != req.TotalSize {
		return fmt.Errorf("%w: source file size changed (expected %d, got %d)", ErrParked, req.TotalSize, fi.Size())
	}

	// 2. Initialize upload with organizer (idempotent)
	initResp, err := u.client.InitUpload(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: init upload failed: %v", ErrParked, err)
	}

	if initResp.Status == protocol.StatusCompleted {
		if onProgress != nil {
			onProgress(req.TotalSize, req.TotalSize)
		}
		return nil
	}

	uploadID := initResp.UploadID
	offset := initResp.Offset

	// 3. Open source file read-only
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("%w: open source file failed: %v", ErrParked, err)
	}
	defer func() { _ = srcFile.Close() }()

	if onProgress != nil {
		onProgress(offset, req.TotalSize)
	}

	// 4. Chunk upload loop
	for offset < req.TotalSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Re-verify file size hasn't changed mid-upload (e.g. user verify / delete in torrent client)
		curFi, err := srcFile.Stat()
		if err != nil || curFi.Size() != req.TotalSize {
			return fmt.Errorf("%w: source file mutated during upload: %v", ErrParked, err)
		}

		success := false
		retries := 0

		for retries <= u.maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			remaining := req.TotalSize - offset
			if remaining <= 0 {
				success = true
				break
			}
			curChunkSize := u.chunkSize
			if remaining < curChunkSize {
				curChunkSize = remaining
			}

			sectionReader := io.NewSectionReader(srcFile, offset, curChunkSize)

			newOffset, uploadErr := u.client.UploadChunk(ctx, uploadID, offset, sectionReader, curChunkSize, "")
			if uploadErr == nil {
				offset = newOffset
				if onProgress != nil {
					onProgress(offset, req.TotalSize)
				}
				success = true
				break
			}

			var conflictErr ErrUploadOffsetConflict
			if errors.As(uploadErr, &conflictErr) {
				// Reconcile cursor with server's authoritative offset
				offset = conflictErr.ServerOffset
				if onProgress != nil {
					onProgress(offset, req.TotalSize)
				}
				// Break out of retry loop to recompute chunk bounds from new offset
				success = true
				break
			}

			// Network / server error: query server offset to calibrate
			retries++
			if retries > u.maxRetries {
				break
			}

			// Exponential backoff with jitter
			sleepDuration := u.calculateBackoff(retries)
			timer := time.NewTimer(sleepDuration)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}

			// Re-probe server offset before retry
			if status, getErr := u.client.GetUpload(ctx, uploadID); getErr == nil {
				if status.Status == protocol.StatusCompleted {
					if onProgress != nil {
						onProgress(req.TotalSize, req.TotalSize)
					}
					return nil
				}
				offset = status.Offset
			}
		}

		if !success {
			return fmt.Errorf("%w: max retries reached uploading chunk at offset %d", ErrParked, offset)
		}
	}

	// 5. Finalize upload
	finishReq := &UploadFinishRequest{
		TorrentID:    req.TorrentID,
		RelativePath: req.RelativePath,
	}
	if err := u.client.FinishUpload(ctx, uploadID, finishReq); err != nil {
		return fmt.Errorf("%w: finish upload failed: %v", ErrParked, err)
	}

	if onProgress != nil {
		onProgress(req.TotalSize, req.TotalSize)
	}
	return nil
}

func (u *Uploader) calculateBackoff(retry int) time.Duration {
	// Base * 2^(retry-1), capped at 30s, with jitter
	multiplier := int64(1) << (retry - 1)
	if multiplier > 30 {
		multiplier = 30
	}
	d := time.Duration(multiplier) * u.backoffBase
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(d / 4)))
	return d + jitter
}
