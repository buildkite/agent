package dockerbootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	base := t.TempDir()
	contextDir := filepath.Join(base, "context")
	if err := os.Mkdir(contextDir, 0o700); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		ContextEnv:                         contextDir,
		"BUILDKITE_ENV_FILE":               filepath.Join(contextDir, "env"),
		"BUILDKITE_ENV_JSON_FILE":          filepath.Join(contextDir, "env.json"),
		"BUILDKITE_AGENT_JOB_TIMEOUT_FILE": filepath.Join(contextDir, "timeout"),
		"BUILDKITE_BUILD_PATH":             filepath.Join(base, "builds"),
		"BUILDKITE_CANCEL_SIGNAL_TIMEOUT":  "200ms",
		"BUILDKITE_CANCEL_SIGNAL":          "SIGTERM",
		"BUILDKITE_AGENT_ACCESS_TOKEN":     "private-token",
		"MULTILINE":                        "  hello\n世界\n",
		"PATH":                             "/host/path",
		"HOME":                             "/host/home",
		"HOST_SECRET":                      "do-not-forward",
	}
	data, _ := json.Marshal(map[string]string{"MULTILINE": values["MULTILINE"], "PATH": values["PATH"], "HOME": values["HOME"]})
	if err := os.WriteFile(values["BUILDKITE_ENV_JSON_FILE"], data, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(base, "buildkite-agent")
	if err := os.WriteFile(binary, []byte("test binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Image: DefaultImage, Binary: binary, CleanupMargin: 100 * time.Millisecond, OperationTimeout: time.Second, PullTimeout: time.Second}
	for name, value := range values {
		cfg.Environment = append(cfg.Environment, name+"="+value)
	}
	return cfg
}

type fakeClient struct {
	mu    sync.Mutex
	calls [][]string
	run   func(context.Context, []string, map[string]string, io.Writer, io.Writer) (int, error)
}

func (f *fakeClient) Run(ctx context.Context, args []string, env map[string]string, out, stderr io.Writer) (int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(args))
	f.mu.Unlock()
	if args[0] == "image" && slices.Contains(args, "--format") {
		_, _ = fmt.Fprintln(out, "sha256:"+strings.Repeat("a", 64)+" []")
	}
	if f.run != nil {
		return f.run(ctx, args, env, out, stderr)
	}
	return 0, nil
}

func (f *fakeClient) called(command string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call[0] == command {
			return true
		}
	}
	return false
}

func TestEnvironmentAndMounts(t *testing.T) {
	cfg := testConfig(t)
	job, err := prepare(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := job.env["MULTILINE"]; got != "  hello\n世界\n" {
		t.Fatalf("multiline value changed: %q", got)
	}
	if job.env["BUILDKITE_AGENT_ACCESS_TOKEN"] != "private-token" {
		t.Fatal("missing token")
	}
	for _, name := range []string{"HOST_SECRET", "HOME", "PATH", ContextEnv} {
		if _, ok := job.env[name]; ok {
			t.Errorf("forwarded host variable %s", name)
		}
	}
	args := strings.Join(job.args, " ")
	for _, forbidden := range []string{"private-token", "hello", "/var/run/docker.sock"} {
		if strings.Contains(args, forbidden) {
			t.Errorf("unexpected value in args: %s", forbidden)
		}
	}
	if !strings.Contains(args, "dst="+containerBinary+",readonly") {
		t.Fatal("missing read-only agent mount")
	}
	if job.env["BUILDKITE_CANCEL_SIGNAL_TIMEOUT"] != "100ms" {
		t.Fatal("inner cancellation budget not reduced")
	}
}

func TestValidation(t *testing.T) {
	for _, tc := range []struct{ name, variable, value string }{
		{"short cancellation", "BUILDKITE_CANCEL_SIGNAL_TIMEOUT", "50ms"},
		{"shared context", ContextEnv, "/tmp"},
		{"foreign timeout", "BUILDKITE_AGENT_JOB_TIMEOUT_FILE", "/other/job"},
		{"broad build mount", "BUILDKITE_BUILD_PATH", "/tmp"},
		{"relative build", "BUILDKITE_BUILD_PATH", "builds"},
		{"loopback API", "BUILDKITE_AGENT_ENDPOINT", "http://127.0.0.1:3000"},
		{"bad name", "BUILDKITE_BAD\nNAME", "secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Environment = append(cfg.Environment, tc.variable+"="+tc.value)
			if _, err := prepare(cfg); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestExitStatusAndOutput(t *testing.T) {
	for _, want := range []int{0, 1, 42, 125, 137, 143, 255} {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			cfg := testConfig(t)
			f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, out, stderr io.Writer) (int, error) {
				if args[0] == "start" {
					_, _ = fmt.Fprint(out, "job stdout")
					_, _ = fmt.Fprint(stderr, "job stderr")
					return want, nil
				}
				return 0, nil
			}}
			var out, stderr bytes.Buffer
			code, err := (Runner{Client: f, Stdout: &out, Stderr: &stderr}).Run(t.Context(), cfg)
			if code != want || err != nil {
				t.Fatalf("got (%d, %v), want %d", code, err, want)
			}
			if !strings.Contains(out.String(), "job stdout") || !strings.Contains(stderr.String(), "job stderr") {
				t.Fatal("lost output")
			}
			if !f.called("rm") {
				t.Fatal("container not removed")
			}
		})
	}
}

func TestPartialFailureCleanup(t *testing.T) {
	for _, command := range []string{"info", "pull", "create", "start"} {
		t.Run(command, func(t *testing.T) {
			f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
				if command == "pull" && args[0] == "image" {
					return 1, nil
				}
				if args[0] == command {
					return 0, errors.New("injected failure")
				}
				return 0, nil
			}}
			code, err := (Runner{Client: f}).Run(t.Context(), testConfig(t))
			if code != SetupFailure || err == nil {
				t.Fatalf("got (%d, %v)", code, err)
			}
			if want := command == "create" || command == "start"; f.called("rm") != want {
				t.Fatalf("cleanup called = %t, want %t", f.called("rm"), want)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	for _, graceful := range []bool{true, false} {
		t.Run(fmt.Sprintf("graceful=%t", graceful), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			signaled := make(chan struct{})
			f := &fakeClient{run: func(ctx context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
				switch args[0] {
				case "start":
					cancel()
					if graceful {
						<-signaled
						return 143, nil
					}
					<-ctx.Done()
					return 0, ctx.Err()
				case "kill":
					if !slices.Contains(args, "SIGTERM") {
						t.Error("missing cancellation signal")
					}
					close(signaled)
				}
				return 0, nil
			}}
			code, err := (Runner{Client: f}).Run(ctx, testConfig(t))
			want := 137
			if graceful {
				want = 143
			}
			if err != nil || code != want {
				t.Fatalf("got (%d, %v), want %d", code, err, want)
			}
			if !f.called("rm") {
				t.Fatal("container not removed")
			}
		})
	}
}

func TestAlreadyRemovedIsSuccess(t *testing.T) {
	f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
		if args[0] == "rm" {
			return 1, nil
		}
		return 0, nil
	}}
	if code, err := (Runner{Client: f}).Run(t.Context(), testConfig(t)); code != 0 || err != nil {
		t.Fatalf("got (%d, %v)", code, err)
	}
	if !f.called("ps") {
		t.Fatal("missing removal verification")
	}
}

func TestCancellationDuringSetup(t *testing.T) {
	for _, phase := range []string{"pull", "create"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &fakeClient{run: func(ctx context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
				if phase == "pull" && args[0] == "image" {
					return 1, nil
				}
				if args[0] == phase {
					cancel()
					<-ctx.Done()
					return 0, ctx.Err()
				}
				if args[0] == "rm" && ctx.Err() != nil {
					t.Error("cleanup inherited cancelled setup context")
				}
				return 0, nil
			}}
			code, err := (Runner{Client: f}).Run(ctx, testConfig(t))
			if code != SetupFailure || err == nil {
				t.Fatalf("got (%d, %v)", code, err)
			}
			if f.called("start") {
				t.Fatal("started a cancelled job")
			}
			if f.called("rm") != (phase == "create") {
				t.Fatal("incorrect partial-create cleanup")
			}
		})
	}
}

func TestStartupTimeout(t *testing.T) {
	cfg := testConfig(t)
	cfg.OperationTimeout = 20 * time.Millisecond
	f := &fakeClient{run: func(ctx context.Context, args []string, _ map[string]string, _, _ io.Writer) (int, error) {
		if args[0] == "start" {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		return 0, nil
	}}
	code, err := (Runner{Client: f}).Run(t.Context(), cfg)
	if code != SetupFailure || err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("got (%d, %v)", code, err)
	}
	if !f.called("rm") {
		t.Fatal("startup timeout left container behind")
	}
}

func TestRuntimeFailureAndOOMDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		exit, want  int
	}{
		{"start failed", `{"Status":"created"}`, 1, SetupFailure},
		{"lost attachment", `{"Running":true}`, 1, SetupFailure},
		{"OOM", `{"OOMKilled":true,"Status":"exited"}`, 137, 137},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeClient{run: func(_ context.Context, args []string, _ map[string]string, out, _ io.Writer) (int, error) {
				if args[0] == "start" {
					return tc.exit, nil
				}
				if args[0] == "inspect" {
					_, _ = fmt.Fprint(out, tc.state)
				}
				return 0, nil
			}}
			var stderr bytes.Buffer
			code, _ := (Runner{Client: f, Stderr: &stderr}).Run(t.Context(), testConfig(t))
			if code != tc.want {
				t.Fatalf("got %d, want %d", code, tc.want)
			}
			if tc.name == "OOM" && !strings.Contains(stderr.String(), "OOM-killed") {
				t.Fatal("missing OOM diagnostic")
			}
		})
	}
}
