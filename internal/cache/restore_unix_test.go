//go:build unix

package cache

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
	"golang.org/x/sys/unix"
)

func TestRunRestoreDownloadReturnsSuccessAfterCancellation(t *testing.T) {
	for _, cause := range []error{errDownloadTimeout, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			reg, apiClient, cfg := setupExecTest(t)
			writeFile(t, "out/file", "cached")
			if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
				t.Fatal(err)
			}
			writeFile(t, "out/file", "original")
			bucket, _ := url.Parse(cfg.BucketURL)
			var blob string
			reg.mu.Lock()
			for _, entry := range reg.entries {
				blob = filepath.Join(bucket.Path, entry.Blobs[0].Digest.Value)
			}
			reg.mu.Unlock()
			metaPath := blob + ".attrs.json"
			if err := os.Remove(metaPath); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(metaPath, 0o600); err != nil {
				t.Fatal(err)
			}
			tempRoot := t.TempDir()
			t.Setenv("TMPDIR", tempRoot)
			cfg.RestoreDownloadTimeout = time.Second
			cfg.FailOnError = true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- RunRestore(ctx, logger.Discard, apiClient, cfg) }()

			// A FIFO writer can open only after the downloader reaches its
			// post-copy metadata read. Nonblocking open also detects early errors
			// without leaving the test stuck waiting for a reader that never comes.
			var meta *os.File
			until := time.Now().Add(5 * time.Second)
			for meta == nil {
				fd, err := unix.Open(metaPath, unix.O_WRONLY|unix.O_NONBLOCK, 0)
				if err == nil {
					meta = os.NewFile(uintptr(fd), metaPath)
					break
				}
				if !errors.Is(err, unix.ENXIO) || time.Now().After(until) {
					t.Fatalf("waiting for post-copy metadata read: %v", err)
				}
				select {
				case err := <-done:
					t.Fatalf("restore returned before reaching metadata: %v", err)
				default:
					time.Sleep(time.Millisecond)
				}
			}
			defer func() { _ = meta.Close() }()
			if cause == context.Canceled {
				cancel()
			} else {
				time.Sleep(1100 * time.Millisecond)
			}
			// The downloader ignores cancellation during this blocking operation
			// and returns nil after it. The shared boundary must reject that late
			// success, without letting verification/extraction mutate targets.
			if _, err := meta.Write([]byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			_ = meta.Close()
			err := <-done
			if !errors.Is(err, cause) {
				t.Fatalf("late successful download = %v, want %v", err, cause)
			}
			if got := readFile(t, "out/file"); got != "original" {
				t.Fatalf("late download mutated target: %q", got)
			}
			if entries, err := os.ReadDir(tempRoot); err != nil || len(entries) != 0 {
				t.Fatalf("temporary download remains: %v, %v", entries, err)
			}
			if _, err := os.Stat(blob); err != nil {
				t.Fatalf("remote blob removed: %v", err)
			}
			reg.mu.Lock()
			defer reg.mu.Unlock()
			if len(reg.entries) != 1 {
				t.Fatal("remote entry removed")
			}
		})
	}
}
