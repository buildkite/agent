package clicommand

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strconv"
	"testing"

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

func TestListJobRedactions(t *testing.T) {
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
	mux.Add("from-secret-get")

	got, err := listJobRedactions(t.Context())
	if err != nil {
		t.Fatalf("listJobRedactions() error = %v", err)
	}
	if len(got) != 1 || got[0] != "from-secret-get" {
		t.Errorf("listJobRedactions() = %q, want [from-secret-get]", got)
	}
}

func TestCacheExecWithoutJobAPIRunsUncached(t *testing.T) {
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
			t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
			t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")
			if err := os.WriteFile("cache.yml", []byte("caches:\n  - name: build\n    cache_key: [v1]\n    target_paths: [out]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := *CacheExecCommand
			app := &cli.Command{Commands: []*cli.Command{&cmd}}
			// The endpoint is unreachable: without the job's secrets, exec must not touch the cache.
			args := []string{"buildkite-agent", "exec", "--endpoint", "http://127.0.0.1:1", "--agent-access-token", "token", "--cache-config-file", "cache.yml", "--name", "build", "--cache-fail-on-error=" + strconv.FormatBool(test.failOnError), "--", "sh", "-c", "touch ran"}
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
