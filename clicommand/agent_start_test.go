package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/core"
	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
)

func TestAgentStartFeatures_OpenTelemetryTracing(t *testing.T) {
	t.Parallel()

	features := AgentStartConfig{OpenTelemetryTracing: true}.Features(t.Context())
	if !slices.Contains(features, "opentelemetry-tracing") {
		t.Fatalf("Features() = %v, want opentelemetry-tracing", features)
	}
}

func setupHooksPath(t *testing.T) (string, func()) {
	t.Helper()

	hooksPath, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	return hooksPath, func() { _ = os.RemoveAll(hooksPath) }
}

func writeAgentHook(t *testing.T, dir, hookName, fixtureName string) string {
	t.Helper()

	filename := hookName
	fixtureFilename := fixtureName + ".sh"
	if runtime.GOOS == "windows" {
		filename = hookName + ".bat"
		fixtureFilename = fixtureName + ".bat"
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() = %v", err)
	}
	fixturePath := filepath.Join(wd, "..", "test", "fixtures", "agent-hook", fixtureFilename)
	script, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", fixturePath, err)
	}

	filepath := filepath.Join(dir, filename)
	t.Logf("Creating %q from %q", filepath, fixturePath)
	if err := os.WriteFile(filepath, script, 0o755); err != nil {
		t.Fatalf("%+v", err)
	}
	return filepath
}

func testAgentWorker(id, name string) *agent.AgentWorker {
	return agent.NewAgentWorker(
		logger.Discard,
		&api.AgentRegisterResponse{UUID: id, Name: name},
		nil,
		api.NewClient(logger.Discard, api.Config{}),
		agent.AgentWorkerConfig{},
	)
}

func TestAgentStartupHook(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:    hooksPath,
			GlobalConfig: GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	t.Run("with agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-startup", "hello-world")
		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] hello world",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with no agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg("zxczxczxc"), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})
}

func TestAgentStartupHookWithAdditionalPaths(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath, additionalHooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:            hooksPath,
			AdditionalHooksPaths: []string{additionalHooksPath},
			GlobalConfig:         GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	t.Run("with additional agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		filepath := writeAgentHook(t, hooksPath, "agent-startup", "hello-new-world")
		defer closer()

		additionalHooksPath, additionalCloser := setupHooksPath(t)
		addFilepath := writeAgentHook(t, additionalHooksPath, "agent-startup", "hello-additional-world")
		defer additionalCloser()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath, additionalHooksPath), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] hello new world",
			"[info] " + prompt + " " + addFilepath,
			"[info] hello additional world",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})
}

func TestAgentStartupHookEnv(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc      string
		workers   []*agent.AgentWorker
		wantIDs   string
		wantNames string
	}{
		{
			desc: "empty",
		},
		{
			desc:      "single agent",
			workers:   []*agent.AgentWorker{testAgentWorker("agent-123", "test-agent-1")},
			wantIDs:   "agent-123",
			wantNames: "test-agent-1",
		},
		{
			desc: "multiple agents",
			workers: []*agent.AgentWorker{
				testAgentWorker("agent-123", "test-agent-1"),
				testAgentWorker("agent-456", "test-agent-2"),
			},
			wantIDs:   "agent-123,agent-456",
			wantNames: "test-agent-1,test-agent-2",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			env := agentLifecycleHookEnv(tc.workers)
			gotIDs, hasIDs := env.Get("BUILDKITE_AGENT_IDS")
			if !hasIDs {
				t.Fatal("BUILDKITE_AGENT_IDS is not set")
			}
			if got := gotIDs; got != tc.wantIDs {
				t.Errorf("BUILDKITE_AGENT_IDS = %q, want %q", got, tc.wantIDs)
			}
			gotNames, hasNames := env.Get("BUILDKITE_AGENT_NAMES")
			if !hasNames {
				t.Fatal("BUILDKITE_AGENT_NAMES is not set")
			}
			if got := gotNames; got != tc.wantNames {
				t.Errorf("BUILDKITE_AGENT_NAMES = %q, want %q", got, tc.wantNames)
			}
		})
	}
}

func TestAgentStartupHookWithRegisteredAgentsEnv(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:    hooksPath,
			GlobalConfig: GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	hooksPath, closer := setupHooksPath(t)
	defer closer()

	filepath := writeAgentHook(t, hooksPath, "agent-startup", "env-hook")

	log := logger.NewBuffer()
	err := agentStartupHook(log, cfg(hooksPath), []*agent.AgentWorker{
		testAgentWorker("agent-123", "test-agent-1"),
		testAgentWorker("agent-456", "test-agent-2"),
	})
	if err != nil {
		t.Fatalf("%+v", log.Messages)
	}
	if diff := cmp.Diff(log.Messages, []string{
		"[info] " + prompt + " " + filepath,
		"[info] ids=agent-123,agent-456",
		"[info] names=test-agent-1,test-agent-2",
	}); diff != "" {
		t.Errorf("log.Messages diff (-got +want):\n%s", diff)
	}
}

func TestAgentShutdownHook(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:    hooksPath,
			GlobalConfig: GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	t.Run("with agent-shutdown hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-shutdown", "hello-world")
		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath), nil)

		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] hello world",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with no agent-shutdown hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath), nil)
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg("zxczxczxc"), nil)
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with registered agents env", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		filepath := writeAgentHook(t, hooksPath, "agent-shutdown", "env-hook")

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath), []*agent.AgentWorker{
			testAgentWorker("agent-123", "test-agent-1"),
			testAgentWorker("agent-456", "test-agent-2"),
		})

		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] ids=agent-123,agent-456",
			"[info] names=test-agent-1,test-agent-2",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})
}

func TestAgentStartRegistrationRejected(t *testing.T) {
	// Prevent token scrubbing from re-executing the test process.
	t.Setenv("BUILDKITE_AGENT_TOKEN", "")

	for _, tc := range []struct {
		name     string
		status   int
		message  string
		wantExit int
	}{
		{
			name:     "job unacquirable",
			status:   http.StatusUnprocessableEntity,
			message:  "Job is not eligible for job acquisition registration",
			wantExit: 27,
		},
		{
			name:     "other validation failure",
			status:   http.StatusUnprocessableEntity,
			message:  "Job acquisition tokens require acquire-job mode",
			wantExit: 1,
		},
		{
			name:     "invalid token",
			status:   http.StatusUnauthorized,
			message:  "Invalid job acquisition token",
			wantExit: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/register" {
					t.Errorf("request = %s %s, want POST /register", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"message":%q}`, tc.message)
			}))
			defer server.Close()

			configPath := filepath.Join(t.TempDir(), "buildkite-agent.cfg")
			if err := os.WriteFile(configPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			// Clone the command: Run mutates it.
			cmd := *AgentStartCommand
			app := &cli.Command{
				Name:           "buildkite-agent",
				Commands:       []*cli.Command{&cmd},
				ExitErrHandler: func(context.Context, *cli.Command, error) {},
			}
			err := app.Run(t.Context(), []string{
				"buildkite-agent", "start",
				"--config", configPath,
				"--token", "test-token",
				"--endpoint", server.URL,
				"--build-path", t.TempDir(),
				"--acquire-job", "job-123",
			})
			if err == nil {
				t.Fatal("agent start succeeded, want registration failure")
			}
			gotExit := 1
			var exitErr cli.ExitCoder
			if errors.As(err, &exitErr) {
				gotExit = exitErr.ExitCode()
			}
			if gotExit != tc.wantExit {
				t.Errorf("exit code = %d, want %d (error: %v)", gotExit, tc.wantExit, err)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("requests = %d, want 1 (no retries or job acquisition)", got)
			}
		})
	}
}

func TestAgentStartPausedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		env    map[string]string
		config string
		want   string
	}{
		{name: "missing", args: []string{"--start-paused"}, want: "whole minutes from 1 to 10080"},
		{name: "zero", args: []string{"--start-paused", "--start-paused-timeout-in-minutes=0"}, want: "whole minutes from 1 to 10080"},
		{name: "negative", args: []string{"--start-paused", "--start-paused-timeout-in-minutes=-1"}, want: "whole minutes from 1 to 10080"},
		{name: "above cap", args: []string{"--start-paused", "--start-paused-timeout-in-minutes=10081"}, want: "whole minutes from 1 to 10080"},
		{name: "CLI malformed", args: []string{"--start-paused-timeout-in-minutes=1h"}, want: "1h"},
		{name: "CLI overflow", args: []string{"--start-paused-timeout-in-minutes=999999999999999999999"}, want: "999999999999999999999"},
		{name: "env malformed bool", env: map[string]string{"BUILDKITE_AGENT_START_PAUSED": "yes"}, want: "yes"},
		{name: "env malformed minutes", env: map[string]string{"BUILDKITE_AGENT_START_PAUSED_TIMEOUT_MINUTES": "1.5"}, want: "1.5"},
		{name: "env missing", env: map[string]string{"BUILDKITE_AGENT_START_PAUSED": "true"}, want: "whole minutes from 1 to 10080"},
		{name: "orphan", args: []string{"--start-paused-timeout-in-minutes=60"}, want: "enable --start-paused or remove"},
		{name: "explicit false", args: []string{"--start-paused=false"}, config: "start-paused=true\nstart-paused-timeout-in-minutes=60", want: "enable --start-paused or remove"},
		{name: "acquire", args: []string{"--start-paused", "--start-paused-timeout-in-minutes=60", "--acquire-job=job-123"}, want: "remove one"},
		{name: "file bool", config: "start-paused=yes", want: "expected true or false"},
		{name: "file minutes", config: "start-paused=true\nstart-paused-timeout-in-minutes=1h", want: "1h"},
		{name: "file fractional", config: "start-paused=true\nstart-paused-timeout-in-minutes=1.5", want: "1.5"},
		{name: "file overflow", config: "start-paused=true\nstart-paused-timeout-in-minutes=999999999999999999999", want: "whole minutes from 1 to 10080"},
		{name: "file disabled malformed duration", config: "start-paused=false\nstart-paused-timeout-in-minutes=1h", want: "enable --start-paused or remove"},
		{name: "file malformed bool and duration", config: "start-paused=yes\nstart-paused-timeout-in-minutes=1h", want: "expected true or false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"message":"unexpected registration"}`)
			})
			err := runPausedAgentStart(t, handler, tc.config, tc.env, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if got := requests.Load(); got != 0 {
				t.Errorf("requests = %d, want zero", got)
			}
		})
	}
}

func TestAgentStartPausedAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spawn int
		body  string
		code  int
		want  string
	}{
		{name: "rejected", spawn: 1, code: 422, body: `{"message":"startup pause unavailable"}`, want: "startup pause unavailable"},
		{name: "unsupported server", spawn: 1, code: 201, body: `{}`, want: "did not acknowledge"},
		{name: "missing deadline", spawn: 1, code: 201, body: `{"start_paused_at":"2026-11-08T01:00:00Z"}`, want: "did not acknowledge"},
		{name: "zero start", spawn: 1, code: 201, body: `{"start_paused_at":"0001-01-01T00:00:00Z","start_paused_until":"0001-01-01T01:00:00Z"}`, want: "did not acknowledge"},
		{name: "mismatched minutes", spawn: 1, code: 201, body: `{"start_paused_at":"2026-11-08T01:00:00Z","start_paused_until":"2026-11-08T01:01:00Z"}`, want: "did not acknowledge"},
		{name: "negative interval", spawn: 1, code: 201, body: `{"start_paused_at":"2026-11-08T01:00:00Z","start_paused_until":"2026-11-08T00:00:00Z"}`, want: "did not acknowledge"},
		{name: "second worker unsupported", spawn: 2, code: 201, body: `{}`, want: "did not acknowledge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks := t.TempDir()
			marker := filepath.Join(t.TempDir(), "startup-ran")
			writePausedStartupHook(t, hooks, marker)
			var requests atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/register" {
					t.Errorf("unexpected session request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				if tc.spawn == 2 && n == 1 {
					_, _ = io.WriteString(w, `{"start_paused_at":"2026-11-08T01:00:00Z","start_paused_until":"2026-11-08T02:00:00Z"}`)
					return
				}
				_, _ = io.WriteString(w, tc.body)
			})
			err := runPausedAgentStart(t, handler, "", nil, "--start-paused", "--start-paused-timeout-in-minutes=60", fmt.Sprintf("--spawn=%d", tc.spawn), "--hooks-path", hooks)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if got := requests.Load(); got != int32(tc.spawn) {
				t.Errorf("requests = %d, want %d registrations only, without retry", got, tc.spawn)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("startup hook marker exists or could not be checked: %v", err)
			}
		})
	}
}

func TestAgentStartPausedPayload(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		env     map[string]string
		config  string
		minutes int
	}{
		{name: "default off"},
		{name: "CLI minimum", args: []string{"--start-paused", "--start-paused-timeout-in-minutes=1"}, minutes: 1},
		{name: "file maximum", config: "start-paused=true\nstart-paused-timeout-in-minutes=10080", minutes: 10080},
		{name: "CLI overrides malformed file", args: []string{"--start-paused", "--start-paused-timeout-in-minutes=60"}, config: "start-paused=yes\nstart-paused-timeout-in-minutes=1h", minutes: 60},
		{name: "env overrides file", env: map[string]string{"BUILDKITE_AGENT_START_PAUSED": "true", "BUILDKITE_AGENT_START_PAUSED_TIMEOUT_MINUTES": "7"}, config: "start-paused=yes\nstart-paused-timeout-in-minutes=1h", minutes: 7},
		{name: "CLI overrides env", args: []string{"--start-paused-timeout-in-minutes=3"}, env: map[string]string{"BUILDKITE_AGENT_START_PAUSED": "true", "BUILDKITE_AGENT_START_PAUSED_TIMEOUT_MINUTES": "7"}, minutes: 3},
		{name: "explicit false overrides file", args: []string{"--start-paused=false"}, config: "start-paused=yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks := t.TempDir()
			marker := filepath.Join(t.TempDir(), "startup-ran")
			writePausedStartupHook(t, hooks, marker)
			var requests atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/register" {
					t.Errorf("unexpected session request %s", r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if tc.minutes == 0 {
					for _, key := range []string{"start_paused", "start_paused_timeout_in_minutes"} {
						if _, ok := payload[key]; ok {
							t.Errorf("default-off payload contains %s", key)
						}
					}
				} else if payload["start_paused"] != true || payload["start_paused_timeout_in_minutes"] != float64(tc.minutes) {
					t.Errorf("startup pause payload = %v, want true and %d minutes", payload, tc.minutes)
				}
				start := time.Date(2026, 11, 8, 1, 0, 0, 123456000, time.UTC)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "agent-123", "name": "test-agent", "access_token": "session-token",
					"start_paused_at": start, "start_paused_until": start.Add(time.Duration(tc.minutes) * time.Minute),
				})
			})
			err := runPausedAgentStart(t, handler, tc.config, tc.env, append(tc.args, "--hooks-path", hooks)...)
			if err == nil || !strings.Contains(err.Error(), "failed to run startup hook") {
				t.Fatalf("error = %v, want deliberate hook failure after accepted registration", err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Errorf("accepted registration did not reach startup hook: %v", err)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("requests = %d, want one registration", got)
			}
		})
	}
}

func runPausedAgentStart(t *testing.T, handler http.Handler, config string, env map[string]string, args ...string) error {
	t.Helper()
	t.Setenv("BUILDKITE_AGENT_TOKEN", "")
	for _, key := range []string{"BUILDKITE_AGENT_START_PAUSED", "BUILDKITE_AGENT_START_PAUSED_TIMEOUT_MINUTES"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "buildkite-agent.cfg")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := *AgentStartCommand
	cmd.Flags = make([]cli.Flag, len(AgentStartCommand.Flags))
	for i, flag := range AgentStartCommand.Flags {
		copy := reflect.New(reflect.TypeOf(flag).Elem())
		copy.Elem().Set(reflect.ValueOf(flag).Elem())
		cmd.Flags[i] = copy.Interface().(cli.Flag)
	}
	app := &cli.Command{
		Name: "buildkite-agent", Commands: []*cli.Command{&cmd},
		Writer: io.Discard, ErrWriter: io.Discard,
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}
	return app.Run(t.Context(), append([]string{
		"buildkite-agent", "start", "--config", configPath, "--token", "test-token",
		"--endpoint", server.URL, "--build-path", t.TempDir(),
	}, args...))
}

func writePausedStartupHook(t *testing.T, hooks, marker string) {
	t.Helper()
	name := "agent-startup"
	script := fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 1\n", marker)
	if runtime.GOOS == "windows" {
		name += ".bat"
		script = fmt.Sprintf("@echo off\ntype nul > %q\nexit /b 1\n", marker)
	}
	if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestAgentStartJobLocked_ExitCode28(t *testing.T) {
	t.Parallel()

	// Test that the CLI command logic returns the correct exit code when ErrJobLocked is returned
	// This simulates what happens in the AgentStartCommand.Run method
	testErr := core.ErrJobLocked

	var cliErr error
	if errors.Is(testErr, core.ErrJobLocked) {
		const jobLockedExitCode = 28
		cliErr = cli.Exit(testErr, jobLockedExitCode)
	}

	var exitErr cli.ExitCoder
	if got := errors.As(cliErr, &exitErr); !got {
		t.Errorf("Expected cli.ExitCoder, got: %v", cliErr)
	}
	if got, want := exitErr.ExitCode(), 28; got != want {
		t.Errorf("Expected exit code 28 for job locked, got: %d", exitErr.ExitCode())
	}
}

func TestAgentStartJobAcquisitionRejected_ExitCode27(t *testing.T) {
	t.Parallel()

	// Test that the CLI command logic returns the correct exit code when ErrJobAcquisitionRejected is returned
	// This simulates what happens in the AgentStartCommand.Run method
	testErr := core.ErrJobAcquisitionRejected

	var cliErr error
	if errors.Is(testErr, core.ErrJobAcquisitionRejected) {
		const acquisitionFailedExitCode = 27
		cliErr = cli.Exit(testErr, acquisitionFailedExitCode)
	}

	var exitErr cli.ExitCoder
	if got := errors.As(cliErr, &exitErr); !got {
		t.Errorf("Expected cli.ExitCoder, got: %v", cliErr)
	}
	if got, want := exitErr.ExitCode(), 27; got != want {
		t.Errorf("Expected exit code 27 for job acquisition rejected, got: %d", exitErr.ExitCode())
	}
}

func TestAgentStartCheckoutOverrideMode(t *testing.T) {
	t.Parallel()

	// AgentStartConfig uses NoCommandEval, so the zero value leaves command-eval
	// enabled; disabling command-eval floors every mode to strict.
	tests := []struct {
		name string
		cfg  AgentStartConfig
		want env.CheckoutOverrideMode
	}{
		{name: "default_from_job", cfg: AgentStartConfig{}, want: env.CheckoutOverrideFromJob},
		{name: "explicit_strict", cfg: AgentStartConfig{CheckoutOverrideMode: "strict"}, want: env.CheckoutOverrideStrict},
		{name: "explicit_none", cfg: AgentStartConfig{CheckoutOverrideMode: "none"}, want: env.CheckoutOverrideNone},
		{name: "no_command_eval_floors_none_to_strict", cfg: AgentStartConfig{CheckoutOverrideMode: "none", NoCommandEval: true}, want: env.CheckoutOverrideStrict},
		{name: "no_command_eval_floors_from_job_to_strict", cfg: AgentStartConfig{CheckoutOverrideMode: "from-job", NoCommandEval: true}, want: env.CheckoutOverrideStrict},
		{name: "no_command_eval_leaves_strict", cfg: AgentStartConfig{CheckoutOverrideMode: "strict", NoCommandEval: true}, want: env.CheckoutOverrideStrict},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.cfg.checkoutOverrideMode()
			if err != nil {
				t.Fatalf("checkoutOverrideMode() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("checkoutOverrideMode() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgentStartCheckoutOverrideModeInvalid(t *testing.T) {
	t.Parallel()
	cfg := AgentStartConfig{CheckoutOverrideMode: "bogus"}
	if _, err := cfg.checkoutOverrideMode(); err == nil {
		t.Error("checkoutOverrideMode() with invalid mode: want error, got nil")
	}
}

func TestBootstrapCheckoutOverrideMode(t *testing.T) {
	t.Parallel()

	// BootstrapConfig uses CommandEval; the zero value disables command-eval and
	// floors the mode to strict, so cases that keep a laxer mode must set
	// CommandEval.
	tests := []struct {
		name string
		cfg  BootstrapConfig
		want env.CheckoutOverrideMode
	}{
		{name: "default_from_job", cfg: BootstrapConfig{CommandEval: true}, want: env.CheckoutOverrideFromJob},
		{name: "explicit_strict", cfg: BootstrapConfig{CheckoutOverrideMode: "strict", CommandEval: true}, want: env.CheckoutOverrideStrict},
		{name: "explicit_none", cfg: BootstrapConfig{CheckoutOverrideMode: "none", CommandEval: true}, want: env.CheckoutOverrideNone},
		{name: "command_eval_disabled_floors_none_to_strict", cfg: BootstrapConfig{CheckoutOverrideMode: "none", CommandEval: false}, want: env.CheckoutOverrideStrict},
		{name: "command_eval_disabled_floors_from_job_to_strict", cfg: BootstrapConfig{CheckoutOverrideMode: "from-job", CommandEval: false}, want: env.CheckoutOverrideStrict},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.cfg.checkoutOverrideMode()
			if err != nil {
				t.Fatalf("checkoutOverrideMode() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("checkoutOverrideMode() = %v, want %v", got, tc.want)
			}
		})
	}
}
