package clicommand

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

func TestCacheExecCommandExitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	err := cacheExecCommand([]string{"sh", "-c", "exit 3"})(io.Discard, io.Discard)
	if want := NewSilentExitError(3); !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
	if err := cacheExecCommand([]string{"true"})(io.Discard, io.Discard); err != nil {
		t.Errorf("successful command returned %v", err)
	}
	if err := cacheExecCommand([]string{"sh", "-c", "kill -9 $$"})(io.Discard, io.Discard); !errors.Is(err, NewSilentExitError(137)) {
		t.Errorf("command killed by SIGKILL: error = %v, want exit status 137", err)
	}
	var exitErr *ExitError
	if err := cacheExecCommand([]string{"./does-not-exist"})(io.Discard, io.Discard); !errors.As(err, &exitErr) || exitErr.Code() != 127 {
		t.Errorf("missing command: error = %v, want exit status 127", err)
	}
}

func TestCacheExecCommandDoesNotWaitForBackgroundProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	start := time.Now()
	// The background process keeps the output pipe open after the command exits.
	err := cacheExecCommand([]string{"sh", "-c", "sleep 10 & echo started"})(&bytes.Buffer{}, io.Discard)
	if err != nil {
		t.Errorf("error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v, want about %v", elapsed, cacheExecWaitDelay)
	}
}

func TestCacheExecArgumentErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "no command", args: []string{"--name", "build"}},
		{name: "no name", args: []string{"--", "true"}},
		{name: "two names", args: []string{"--name", "a", "--name", "b", "--", "true"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := *CacheExecCommand
			app := &cli.Command{Commands: []*cli.Command{&cmd}}
			args := append([]string{"buildkite-agent", "exec", "--agent-access-token", "token"}, test.args...)
			if err := app.Run(t.Context(), args); err == nil {
				t.Error("cache exec succeeded, want an argument error")
			}
		})
	}
}

func TestCacheExecRedactor(t *testing.T) {
	mux := replacer.NewMux(replacer.New(io.Discard, nil, redact.Redacted))
	sock, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv, token, err := jobapi.NewServer(shell.TestingLogger{T: t}, sock, nil, mux)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", srv.SocketPath)
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", token)
	mux.Add("from-secret-get") // known only to the job executor

	// from-step-env stands for a variable exported in the step's command, which only exec's own scan knows.
	got, err := cacheExecRedactor([]string{"from-step-env"})(t.Context(), []jobapi.OutputChunk{
		{Data: []byte("from-secret-get and from-step-env\n")},
	})
	if err != nil {
		t.Fatalf("cacheExecRedactor() error = %v", err)
	}
	var out strings.Builder
	for _, c := range got {
		out.Write(c.Data)
	}
	if got, want := out.String(), "[REDACTED] and [REDACTED]\n"; got != want {
		t.Errorf("redacted output = %q, want %q", got, want)
	}
}

func TestCacheExecWithoutConfigRunsUncached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	for _, test := range []struct {
		name        string
		failOnError bool
	}{
		{name: "runs the command by default"},
		{name: "fails with cache-fail-on-error", failOnError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cmd := *CacheExecCommand
			app := &cli.Command{Commands: []*cli.Command{&cmd}}
			args := []string{"buildkite-agent", "exec", "--agent-access-token", "token", "--name", "build", "--cache-fail-on-error=" + strconv.FormatBool(test.failOnError), "--", "sh", "-c", "touch ran"}
			err := app.Run(t.Context(), args)
			if gotErr := err != nil; gotErr != test.failOnError {
				t.Errorf("cache exec error = %v, want error: %t", err, test.failOnError)
			}
			_, statErr := os.Stat("ran")
			if ran := statErr == nil; ran == test.failOnError {
				t.Errorf("command ran = %t, want %t", ran, !test.failOnError)
			}
		})
	}
}

func TestCacheExecWithoutJobAPISavesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	for _, test := range []struct {
		name        string
		failOnError bool
	}{
		{name: "runs the command"},
		{name: "runs the command, then fails with cache-fail-on-error", failOnError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
			t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")
			if err := os.WriteFile("cache.yml", []byte("caches:\n  - name: build\n    cache_key: [v1]\n    target_paths: [out]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// A registry with no entries: exec may look for a result, but without the job's secrets it must not save one.
			var requests []string
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"message":"Cache entry not found"}`)
			}))
			t.Cleanup(registry.Close)
			cmd := *CacheExecCommand
			app := &cli.Command{Commands: []*cli.Command{&cmd}}
			args := []string{"buildkite-agent", "exec", "--endpoint", registry.URL, "--agent-access-token", "token", "--cache-config-file", "cache.yml", "--name", "build", "--cache-fail-on-error=" + strconv.FormatBool(test.failOnError), "--", "sh", "-c", "mkdir out && touch ran"}
			err := app.Run(t.Context(), args)
			if gotErr := err != nil; gotErr != test.failOnError {
				t.Errorf("cache exec error = %v, want error: %t", err, test.failOnError)
			}
			if test.failOnError && !strings.Contains(err.Error(), "Job API") {
				t.Errorf("cache exec error = %v, want it to be about the Job API", err)
			}
			if _, err := os.Stat("ran"); err != nil {
				t.Errorf("command didn't run: %v", err)
			}
			for _, path := range requests {
				if !strings.HasSuffix(path, "/retrieve") {
					t.Errorf("cache exec requested %s; without the job's secrets it should only look for a result", path)
				}
			}
		})
	}
}
