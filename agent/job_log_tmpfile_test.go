package agent

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/metrics"
)

// jobLogTmpfileTestLines is the number of lines the test bootstrap prints.
// Large enough that output is still being copied out of the PTY when the
// bootstrap process exits.
const jobLogTmpfileTestLines = 5000

// writeChattyBootstrap writes a bootstrap script that prints
// jobLogTmpfileTestLines numbered lines followed by a final marker line, and
// returns the bootstrap-script config value to run it.
func writeChattyBootstrap(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "bootstrap.bat")
		script := fmt.Sprintf("@echo off\r\nfor /L %%%%i in (1,1,%d) do echo line %%%%i\r\necho LAST LINE\r\n", jobLogTmpfileTestLines)
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatalf("os.WriteFile(%q) = %v", path, err)
		}
		return fmt.Sprintf(`cmd.exe /c "%s"`, path)
	}
	path := filepath.Join(dir, "bootstrap.sh")
	script := fmt.Sprintf("#!/bin/sh\ni=1\nwhile [ $i -le %d ]; do echo \"line $i\"; i=$((i+1)); done\necho LAST LINE\n", jobLogTmpfileTestLines)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", path, err)
	}
	return path
}

// jobLogCollector is a minimal fake of the job endpoints used by JobRunner.Run
// that records the uploaded (gzipped) log chunks in order.
type jobLogCollector struct {
	mu     sync.Mutex
	chunks map[int][]byte
}

func newJobLogCollector(t *testing.T) (*httptest.Server, *jobLogCollector) {
	t.Helper()
	c := &jobLogCollector{chunks: make(map[int][]byte)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jobs/{id}", func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(rw, `{"state":"running"}`)
	})
	mux.HandleFunc("PUT /jobs/{id}/start", func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(rw, `{}`)
	})
	mux.HandleFunc("PUT /jobs/{id}/finish", func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(rw, `{}`)
	})
	mux.HandleFunc("POST /jobs/{id}/chunks", func(rw http.ResponseWriter, req *http.Request) {
		var seq int
		if _, err := fmt.Sscan(req.URL.Query().Get("sequence"), &seq); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		zr, err := gzip.NewReader(req.Body)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		b, err := io.ReadAll(zr)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.chunks[seq] = b
		c.mu.Unlock()
		rw.WriteHeader(http.StatusCreated)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, c
}

// log returns the log uploaded so far, in sequence order.
func (c *jobLogCollector) log() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sb strings.Builder
	for seq := 1; seq <= len(c.chunks); seq++ {
		sb.Write(c.chunks[seq])
	}
	return sb.String()
}

func newJobLogTmpfileRunner(t *testing.T, bootstrap string, pty bool) (*JobRunner, *jobLogCollector) {
	t.Helper()
	server, collector := newJobLogCollector(t)
	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})
	jr, err := NewJobRunner(t.Context(), logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {}), apiClient, JobRunnerConfig{
		Job: &api.Job{
			ID:                 "job-log-tmpfile-test",
			Token:              "bkaj_alpacas",
			ChunksMaxSizeBytes: 1024,
			Env:                map[string]string{},
		},
		MetricsScope:      metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}).Scope(nil),
		JobStatusInterval: time.Second,
		AgentConfiguration: AgentConfiguration{
			BootstrapScript:     bootstrap,
			BuildPath:           t.TempDir(),
			RunInPty:            pty,
			EnableJobLogTmpfile: true,
			JobLogPath:          t.TempDir(),
		},
	})
	if err != nil {
		t.Fatalf("NewJobRunner() error = %v", err)
	}
	return jr, collector
}

// TestJobRunner_JobLogTmpfile_ClosedAndRemovedAfterJob checks that the job log
// tmpfile is both closed and deleted once the job finishes. Removing an open
// file fails on Windows, and on Unix it would silently leak one file
// descriptor per job (https://github.com/buildkite/agent/issues/4331).
func TestJobRunner_JobLogTmpfile_ClosedAndRemovedAfterJob(t *testing.T) {
	// NewJobRunner sets BUILDKITE_JOB_LOG_TMPFILE in the process environment,
	// so this test cannot be parallel. t.Setenv restores the original value.
	t.Setenv("BUILDKITE_JOB_LOG_TMPFILE", "")

	tests := []struct {
		name         string
		bootstrap    string
		pty          bool
		wantFullLog  bool
		wantLogMatch string
	}{
		{
			name:        "bootstrap_runs",
			bootstrap:   writeChattyBootstrap(t),
			wantFullLog: true,
		},
		{
			name:        "bootstrap_runs_in_pty",
			bootstrap:   writeChattyBootstrap(t),
			pty:         true,
			wantFullLog: true,
		},
		{
			// The bootstrap never starts, so process.Done() never closes.
			// The tmpfile must still be cleaned up.
			name:         "bootstrap_fails_to_start",
			bootstrap:    filepath.Join(t.TempDir(), "does-not-exist"),
			wantLogMatch: "Error running job",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pty && runtime.GOOS == "windows" {
				t.Skip("PTY is not supported on Windows")
			}

			jr, collector := newJobLogTmpfileRunner(t, tc.bootstrap, tc.pty)
			tmpFile := jr.jobLogTmpFile
			if tmpFile == nil {
				t.Fatal("jr.jobLogTmpFile = nil, want a file")
			}
			if got, want := os.Getenv("BUILDKITE_JOB_LOG_TMPFILE"), tmpFile.Name(); got != want {
				t.Errorf("BUILDKITE_JOB_LOG_TMPFILE = %q, want %q", got, want)
			}

			if err := jr.Run(t.Context(), nil); err != nil {
				t.Fatalf("jr.Run() = %v", err)
			}

			// The runner must have closed the file itself. Closing an already
			// closed *os.File returns os.ErrClosed; closing a leaked one
			// succeeds (and closes it, so the test does not leak either).
			if err := tmpFile.Close(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("job log tmpfile was left open by the job runner: tmpFile.Close() = %v, want %v", err, os.ErrClosed)
			}
			if _, err := os.Stat(tmpFile.Name()); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("job log tmpfile still exists after the job: os.Stat(%q) error = %v, want %v", tmpFile.Name(), err, fs.ErrNotExist)
			}

			// The tmpfile must never cost us job log output. Every line the
			// bootstrap printed has to reach the API, including the tail that
			// is copied out of the PTY after the process has exited.
			log := collector.log()
			if tc.wantFullLog {
				if got, want := strings.Count(log, "line "), jobLogTmpfileTestLines; got != want {
					t.Errorf("uploaded job log contains %d numbered lines, want %d", got, want)
				}
				if !strings.Contains(log, "LAST LINE") {
					t.Errorf("uploaded job log is missing the final line; tail = %q", tail(log, 200))
				}
			}
			if tc.wantLogMatch != "" && !strings.Contains(log, tc.wantLogMatch) {
				t.Errorf("uploaded job log = %q, want it to contain %q", log, tc.wantLogMatch)
			}
		})
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// TestBestEffortWriter_FailureDoesNotStopOtherWriters pins the property that
// lets the tmpfile be closed safely: a failing tmpfile write must not surface
// as an error from the combined job log writer, because io.MultiWriter and
// io.Copy stop at the first error and the Buildkite log would lose everything
// after it.
func TestBestEffortWriter_FailureDoesNotStopOtherWriters(t *testing.T) {
	t.Parallel()

	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("closed.Close() = %v", err)
	}

	var primary bytes.Buffer
	w := io.MultiWriter(&primary, &bestEffortWriter{w: closed, logger: logger.Discard, desc: "closed file"})

	want := strings.Repeat("some job log output\n", 100)
	n, err := io.Copy(w, strings.NewReader(want))
	if err != nil {
		t.Errorf("io.Copy() error = %v, want nil", err)
	}
	if n != int64(len(want)) {
		t.Errorf("io.Copy() copied %d bytes, want %d", n, len(want))
	}
	if got := primary.String(); got != want {
		t.Errorf("primary writer received %d bytes, want %d", len(got), len(want))
	}
}
