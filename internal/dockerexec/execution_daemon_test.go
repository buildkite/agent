package dockerexec

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// These tests run real containers, which a fake daemon cannot stand in for.
// They need a local Docker daemon that sees the same filesystem as the test,
// and run only when BUILDKITE_TEST_DOCKER_EXECUTOR is set.

const daemonTestImage = "alpine:3.20"

// fakeBootstrap stands in for `buildkite-agent bootstrap`, reporting what it
// can see from inside the container.
const fakeBootstrap = `package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "bootstrap" {
		fmt.Println("unexpected args:", os.Args)
		os.Exit(2)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)

	wd, _ := os.Getwd()
	fmt.Printf("uid=%d gid=%d\n", os.Getuid(), os.Getgid())
	fmt.Printf("pwd=%s\n", wd)
	fmt.Printf("home=%s\n", os.Getenv("HOME"))
	fmt.Printf("job_var=%s\n", os.Getenv("JOB_VAR"))
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), "probe"), nil, 0o600); err != nil {
		fmt.Println("home not writable:", err)
	} else {
		fmt.Println("home writable")
	}
	if err := os.WriteFile(filepath.Join(wd, "built"), nil, 0o644); err != nil {
		fmt.Println("build path not writable:", err)
	}
	if os.Getenv("WAIT_FOR_SIGTERM") != "" {
		fmt.Println("ready")
		select {
		case <-sig:
			fmt.Println("got SIGTERM")
			os.Exit(42)
		case <-time.After(time.Minute):
		}
	}
	code, _ := strconv.Atoi(os.Getenv("EXIT_CODE"))
	os.Exit(code)
}
`

// newDaemonExecutor prepares an Executor against the local daemon, running
// fakeBootstrap in image.
func newDaemonExecutor(t *testing.T, image string, mutate func(*Config)) (*Executor, *client.Client) {
	t.Helper()
	if os.Getenv("BUILDKITE_TEST_DOCKER_EXECUTOR") == "" {
		t.Skip("set BUILDKITE_TEST_DOCKER_EXECUTOR to run tests against a local Docker daemon")
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("client.New() error = %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	bin, err := buildStaticBinary(t.TempDir(), fakeBootstrap)
	if err != nil {
		t.Fatalf("building fake bootstrap: %v", err)
	}
	dir := t.TempDir()
	cfg := Config{
		Image:         image,
		BuildPath:     filepath.Join(dir, "builds"),
		PluginsPath:   filepath.Join(dir, "plugins"),
		SocketsPath:   filepath.Join(dir, "sockets"),
		JobContextDir: filepath.Join(dir, "job-context"),
		CancelSignal:  process.SIGTERM,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	e, err := prepare(t.Context(), logger.Discard, cfg, preflightDeps{
		client:          cli,
		agentBinary:     bin,
		lookupEnv:       os.LookupEnv,
		dockerConfigDir: dockerConfigDir(),
	})
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	return e, cli
}

func TestDaemon_RunsBootstrapAsTheAgentUser(t *testing.T) {
	t.Parallel()

	for _, pty := range []bool{false, true} {
		t.Run("pty="+strconv.FormatBool(pty), func(t *testing.T) {
			t.Parallel()
			e, cli := newDaemonExecutor(t, daemonTestImage, func(c *Config) { c.RunInPty = pty })
			out := &process.Buffer{}
			x := e.NewExecution(Request{JobID: "daemon-test", Env: []string{"JOB_VAR=hello", "EXIT_CODE=3"}, Output: out})
			cleanupAfterTest(t, x)

			runErr := x.Run(t.Context())
			cleanupErr := x.Cleanup(t.Context())
			if runErr != nil {
				t.Fatalf("Run() error = %v", runErr)
			}
			if cleanupErr != nil {
				t.Fatalf("Cleanup() error = %v", cleanupErr)
			}

			got := strings.ReplaceAll(string(out.ReadAndTruncate()), "\r\n", "\n")
			for _, want := range []string{
				"uid=" + strconv.Itoa(os.Getuid()) + " gid=" + strconv.Itoa(os.Getgid()) + "\n",
				"pwd=" + e.cfg.BuildPath + "\n",
				"home=/tmp/buildkite-home\n",
				"home writable\n",
				"job_var=hello\n",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("output = %q, want it to contain %q", got, want)
				}
			}
			if got, want := x.WaitStatus().ExitStatus(), 3; got != want {
				t.Errorf("WaitStatus().ExitStatus() = %d, want %d", got, want)
			}

			info, err := os.Stat(filepath.Join(e.cfg.BuildPath, "built"))
			if err != nil {
				t.Fatalf("file written in the build path: %v", err)
			}
			if uid := info.Sys().(*syscall.Stat_t).Uid; int(uid) != os.Getuid() {
				t.Errorf("file written in the build path is owned by uid %d, want %d", uid, os.Getuid())
			}
			if _, err := cli.ContainerInspect(t.Context(), x.id, client.ContainerInspectOptions{}); !cerrdefs.IsNotFound(err) {
				t.Errorf("ContainerInspect() after Cleanup error = %v, want not found", err)
			}
		})
	}
}

func TestDaemon_InterruptReachesBootstrap(t *testing.T) {
	t.Parallel()
	e, _ := newDaemonExecutor(t, daemonTestImage, nil)
	out := &process.Buffer{}
	x := e.NewExecution(Request{JobID: "daemon-test", Env: []string{"WAIT_FOR_SIGTERM=1"}, Output: out})
	cleanupAfterTest(t, x)

	wait := runInBackground(t, x)
	var seen strings.Builder
	for deadline := time.Now().Add(30 * time.Second); !strings.Contains(seen.String(), "ready"); {
		if time.Now().After(deadline) {
			t.Fatalf("fake bootstrap did not become ready, output = %q", seen.String())
		}
		seen.Write(out.ReadAndTruncate())
		time.Sleep(10 * time.Millisecond)
	}

	if err := x.Interrupt(); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if err := wait(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := x.WaitStatus().ExitStatus(), 42; got != want {
		t.Errorf("WaitStatus().ExitStatus() = %d, want %d (the fake bootstrap's SIGTERM exit)", got, want)
	}
}

func TestDaemon_CleanupRemovesAnonymousVolumes(t *testing.T) {
	t.Parallel()
	_, cli := newDaemonExecutor(t, daemonTestImage, nil)

	// Make an image that declares a VOLUME, as the official agent image does.
	base, err := cli.ContainerCreate(t.Context(), client.ContainerCreateOptions{Image: daemonTestImage})
	if err != nil {
		t.Fatalf("ContainerCreate() error = %v", err)
	}
	committed, err := cli.ContainerCommit(t.Context(), base.ID, client.ContainerCommitOptions{Changes: []string{"VOLUME /data"}})
	_, _ = cli.ContainerRemove(t.Context(), base.ID, client.ContainerRemoveOptions{Force: true})
	if err != nil {
		t.Fatalf("ContainerCommit() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = cli.ImageRemove(context.WithoutCancel(t.Context()), committed.ID, client.ImageRemoveOptions{Force: true})
	})

	e, _ := newDaemonExecutor(t, committed.ID, nil)
	x := e.NewExecution(Request{JobID: "daemon-test", Output: &process.Buffer{}})
	cleanupAfterTest(t, x)
	if err := x.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	info, err := cli.ContainerInspect(t.Context(), x.id, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("ContainerInspect() error = %v", err)
	}
	var volume string
	for _, m := range info.Container.Mounts {
		if m.Type == mount.TypeVolume && m.Destination == "/data" {
			volume = m.Name
		}
	}
	if volume == "" {
		t.Fatal("container has no anonymous volume at /data")
	}

	if err := x.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := cli.VolumeInspect(t.Context(), volume, client.VolumeInspectOptions{}); !cerrdefs.IsNotFound(err) {
		t.Errorf("VolumeInspect(%s) after Cleanup error = %v, want not found", volume, err)
	}
}

// cleanupAfterTest removes x's container when the test ends, even if it
// fails first. Removing it twice is harmless.
func cleanupAfterTest(t *testing.T, x *Execution) {
	t.Helper()
	// t.Context is cancelled before cleanups run.
	t.Cleanup(func() {
		if err := x.Cleanup(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	})
}
