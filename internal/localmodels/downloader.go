package localmodels

// downloader.go is server/services/local_models/downloader.js: downloads one
// large file safely — into "<file>.part", resuming where an earlier attempt
// stopped, checked against its size and SHA-256, and only then renamed into
// place. A file already in place with the right size is kept (verify checks its
// hash too, for repairs).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Download bounds, unchanged from downloader.js.
const (
	downloadAttempts = 4
	// stallMs drops a connection that delivers nothing for this long, then
	// resumes.
	stallMs = 60_000
	// downloadUserAgent is what Hugging Face and GitHub see.
	downloadUserAgent = "NeoRouter"
)

// retryDelays are the waits between attempts, in milliseconds.
var retryDelays = []int{2000, 5000, 15000}

// File is one file to download: where from, how big and its SHA-256 when known.
// Path is the repository-relative name of a model file (empty for runtime
// assets).
type File struct {
	Path   string `json:"path,omitempty"`
	URL    string `json:"url"`
	Size   *int64 `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

// DownloadOptions tune one download.
type DownloadOptions struct {
	// OnBytes reports progress as a byte delta, including negative deltas when
	// an attempt starts over.
	OnBytes func(delta int64)
	// Verify also checks the hash of a file that is already in place.
	Verify bool
}

// retryableError marks a failure the downloader may try again.
type retryableError struct {
	err   error
	retry bool
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// shouldRetry reports whether err is worth another attempt; errors without the
// mark are retried, like `error.retry !== false`.
func shouldRetry(err error) bool {
	var typed *retryableError
	if errors.As(err, &typed) {
		return typed.retry
	}
	return true
}

func markRetry(err error, retry bool) error {
	return &retryableError{err: err, retry: retry}
}

// fileSize is statSync(file).size, nil when the file is missing.
func fileSize(file string) *int64 {
	info, err := os.Stat(file)
	if err != nil {
		return nil
	}
	size := info.Size()
	return &size
}

// HashFile is the hex SHA-256 of a file.
func HashFile(ctx context.Context, file string) (string, error) {
	handle, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	hash := sha256.New()
	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := handle.Read(buf)
		if n > 0 {
			hash.Write(buf[:n])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// IsComplete reports whether the file is complete, and with verify also whether
// its hash matches.
func IsComplete(ctx context.Context, file string, size *int64, sha string, verify bool) (bool, error) {
	actual := fileSize(file)
	if actual == nil {
		return false, nil
	}
	if size != nil && *actual != *size {
		return false, nil
	}
	if !verify || sha == "" {
		return true, nil
	}
	hash, err := HashFile(ctx, file)
	if err != nil {
		return false, err
	}
	return hash == sha, nil
}

// Download fetches file into dest, resuming where an earlier attempt stopped
// and checking size and hash before the file is put in place. It returns the
// last failure when the file stays broken.
func Download(ctx context.Context, client *http.Client, file File, dest string, opts DownloadOptions) error {
	onBytes := opts.OnBytes
	if onBytes == nil {
		onBytes = func(int64) {}
	}
	if client == nil {
		client = http.DefaultClient
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	complete, err := IsComplete(ctx, dest, file.Size, "", false)
	if err != nil {
		return err
	}
	if complete {
		reported := int64(0)
		if file.Size != nil {
			reported = *file.Size
		} else if actual := fileSize(dest); actual != nil {
			reported = *actual
		}
		onBytes(reported)
		return nil
	}
	part := dest + ".part"
	if existing := fileSize(part); existing != nil {
		onBytes(*existing)
	}
	var lastError error
	for index := 0; index < downloadAttempts; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := downloadAttempt(ctx, client, file, part, onBytes)
		if err == nil {
			if file.Size != nil {
				if actual := fileSize(part); actual == nil || *actual != *file.Size {
					err = markRetry(errors.New("The download ended early."), true)
				}
			}
			if err == nil && file.SHA256 != "" {
				hash, hashErr := HashFile(ctx, part)
				if hashErr != nil {
					err = hashErr
				} else if hash != file.SHA256 {
					if size := fileSize(part); size != nil {
						onBytes(-*size)
					}
					_ = os.Remove(part)
					err = markRetry(errors.New("The downloaded file was damaged."), true)
				}
			}
			if err == nil {
				if renameErr := os.Rename(part, dest); renameErr != nil {
					return renameErr
				}
				return nil
			}
		}
		if ctx.Err() != nil {
			return err
		}
		lastError = err
		if !shouldRetry(err) || index == downloadAttempts-1 {
			break
		}
		wait := retryDelays[len(retryDelays)-1]
		if index < len(retryDelays) {
			wait = retryDelays[index]
		}
		if err := sleepCtx(ctx, time.Duration(wait)*time.Millisecond); err != nil {
			return err
		}
	}
	return lastError
}

// downloadAttempt fetches the rest of the file into part.
func downloadAttempt(ctx context.Context, client *http.Client, file File, part string, onBytes func(int64)) error {
	offset := int64(0)
	if existing := fileSize(part); existing != nil {
		offset = *existing
	}
	if file.Size != nil && offset == *file.Size {
		return nil
	}

	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stalled := make(chan struct{})
	stallTimer := time.AfterFunc(stallMs*time.Millisecond, func() {
		close(stalled)
		cancel()
	})
	defer stallTimer.Stop()

	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, file.URL, nil)
	if err != nil {
		return markRetry(err, false)
	}
	request.Header.Set("User-Agent", downloadUserAgent)
	if offset > 0 {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A stalled connection is retried from where it stopped.
		if isClosed(stalled) {
			return markRetry(errors.New("The download stalled."), true)
		}
		return markRetry(err, true)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return markRetry(fmt.Errorf("Download failed with status %d.", response.StatusCode),
			response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests)
	}

	// A server that ignores the range starts over from the first byte.
	resumed := offset > 0 && response.StatusCode == http.StatusPartialContent
	if !resumed && offset > 0 {
		onBytes(-offset)
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if resumed {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return markRetry(err, false)
	}
	defer out.Close()

	buf := make([]byte, 64*1024)
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			stallTimer.Reset(stallMs * time.Millisecond)
			onBytes(int64(n))
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return markRetry(writeErr, false)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isClosed(stalled) {
				return markRetry(errors.New("The download stalled."), true)
			}
			return markRetry(readErr, true)
		}
	}
}

func isClosed(channel chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

// sleepCtx waits for d, or returns when ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
