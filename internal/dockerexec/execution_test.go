package dockerexec

import (
	"context"
	"encoding/binary"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// newTestExecution returns an execution backed by fake, with every host path
// in a temporary directory, and the buffer its output goes to.
func newTestExecution(t *testing.T, fake *fakeClient, mutate func(*Config)) (*Execution, *process.Buffer) {
	t.Helper()
	dir := t.TempDir()
	cfg := validConfig()
	cfg.BuildPath = filepath.Join(dir, "builds")
	cfg.PluginsPath = filepath.Join(dir, "plugins")
	cfg.SocketsPath = filepath.Join(dir, "sockets")
	cfg.JobContextDir = filepath.Join(dir, "job-context")
	cfg.GitMirrorsPath = ""
	cfg.HooksPaths = nil
	cfg.SigningJWKSFile = ""
	cfg.CancelSignal = process.SIGTERM
	if mutate != nil {
		mutate(&cfg)
	}
	e := &Executor{logger: logger.Discard, client: fake, cfg: cfg, imageID: "sha256:abc", agentBinary: "/usr/bin/buildkite-agent"}
	out := &process.Buffer{}
	return e.NewExecution(Request{JobID: "job-1", Output: out}), out
}

// runInBackground starts x.Run and returns a function that waits for it.
func runInBackground(t *testing.T, x *Execution) func() error {
	t.Helper()
	errC := make(chan error, 1)
	go func() { errC <- x.Run(t.Context()) }()
	return func() error {
		select {
		case err := <-errC:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return within 10s")
			return nil
		}
	}
}

func TestExecution_ReportsTheExitCodeAndOutput(t *testing.T) {
	t.Parallel()
	fake, c := newFakeDaemon(t)
	c.output = []byte("hello from bootstrap\n")
	x, out := newTestExecution(t, fake, func(c *Config) { c.RunInPty = true })

	startedBeforeCreate := false
	create := fake.containerCreate
	fake.containerCreate = func(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
		select {
		case <-x.Started():
			startedBeforeCreate = true
		default:
		}
		return create(ctx, opts)
	}
	start := fake.containerStart
	fake.containerStart = func(ctx context.Context, id string) error {
		err := start(ctx, id)
		c.exitWith(3)
		return err
	}

	if err := x.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !startedBeforeCreate {
		t.Error("Started() was not closed before the container was created")
	}
	if got, want := strings.Join(fake.Calls(), ","), "ContainerCreate,ContainerAttach,ContainerWait,ContainerStart"; got != want {
		t.Errorf("daemon calls = [%s], want [%s]", got, want)
	}
	if got, want := x.WaitStatus().ExitStatus(), 3; got != want {
		t.Errorf("WaitStatus().ExitStatus() = %d, want %d", got, want)
	}
	if x.WaitStatus().Signaled() {
		t.Error("WaitStatus().Signaled() = true, want false")
	}
	if got, want := string(out.ReadAndTruncate()), "hello from bootstrap\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	select {
	case <-x.Done():
	default:
		t.Error("Done() is not closed after Run returned")
	}
}

func TestExecution_DemultiplexesOutputWithoutATTY(t *testing.T) {
	t.Parallel()
	fake, c := newFakeDaemon(t)
	c.output = slices.Concat(stdFrame(1, "to stdout\n"), stdFrame(2, "to stderr\n"))
	x, out := newTestExecution(t, fake, nil)
	start := fake.containerStart
	fake.containerStart = func(ctx context.Context, id string) error {
		err := start(ctx, id)
		c.exitWith(0)
		return err
	}

	if err := x.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := string(out.ReadAndTruncate()), "to stdout\nto stderr\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestExecution_LaunchFailuresAreExecutorErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		breakIt func(*fakeClient)
		wantErr string
		cleanup string
	}{
		{
			name: "create",
			breakIt: func(f *fakeClient) {
				f.containerCreate = func(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
					return client.ContainerCreateResult{}, errors.New("no such image")
				}
			},
			wantErr: "creating the job container: no such image",
			cleanup: "ContainerRemove buildkite-job-job-1-",
		},
		{
			name: "start",
			breakIt: func(f *fakeClient) {
				f.containerStart = func(context.Context, string) error { return errors.New("exec format error") }
			},
			wantErr: "starting the job container: exec format error",
			cleanup: "ContainerRemove cid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake, _ := newFakeDaemon(t)
			tc.breakIt(fake)
			var removeOpts client.ContainerRemoveOptions
			fake.containerRemove = func(_ string, opts client.ContainerRemoveOptions) error {
				removeOpts = opts
				return nil
			}
			x, _ := newTestExecution(t, fake, nil)

			if err := x.Run(t.Context()); err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Run() error = %v, want %q", err, tc.wantErr)
			}
			if err := x.Cleanup(t.Context()); err != nil {
				t.Fatalf("Cleanup() error = %v", err)
			}
			calls := fake.Calls()
			if last := calls[len(calls)-1]; !strings.HasPrefix(last, tc.cleanup) {
				t.Errorf("last daemon call = %q, want prefix %q", last, tc.cleanup)
			}
			if !removeOpts.Force || !removeOpts.RemoveVolumes {
				t.Errorf("remove options = %+v, want Force and RemoveVolumes", removeOpts)
			}
		})
	}
}

func TestExecution_InterruptDuringCreateAbandonsIt(t *testing.T) {
	t.Parallel()
	fake, _ := newFakeDaemon(t)
	creating := make(chan struct{})
	fake.containerCreate = func(ctx context.Context, _ client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
		close(creating)
		<-ctx.Done()
		return client.ContainerCreateResult{}, ctx.Err()
	}
	x, _ := newTestExecution(t, fake, nil)

	wait := runInBackground(t, x)
	<-creating
	if err := x.Interrupt(); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if err := wait(); !errors.Is(err, errCancelledBeforeStart) {
		t.Fatalf("Run() error = %v, want %v", err, errCancelledBeforeStart)
	}
	if err := x.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if got, want := strings.Join(fake.Calls(), ","), "ContainerCreate,ContainerRemove "+x.name; got != want {
		t.Errorf("daemon calls = [%s], want [%s]", got, want)
	}
}

func TestExecution_CancelBeforeStartNeverStarts(t *testing.T) {
	t.Parallel()

	for _, cancel := range []string{"Interrupt", "Terminate"} {
		t.Run(cancel, func(t *testing.T) {
			t.Parallel()
			fake, _ := newFakeDaemon(t)
			x, _ := newTestExecution(t, fake, nil)
			attach := fake.containerAttach
			fake.containerAttach = func(ctx context.Context, id string) (client.ContainerAttachResult, error) {
				if cancel == "Interrupt" {
					_ = x.Interrupt()
				} else {
					_ = x.Terminate()
				}
				return attach(ctx, id)
			}

			// Terminate also stops Run's daemon calls, so the error it
			// returns depends on which call notices first.
			err := x.Run(t.Context())
			if err == nil || (cancel == "Interrupt" && !errors.Is(err, errCancelledBeforeStart)) {
				t.Fatalf("Run() error = %v, want an error", err)
			}
			if calls := fake.Calls(); slices.Contains(calls, "ContainerStart") {
				t.Errorf("daemon calls = %v, want no ContainerStart", calls)
			}
		})
	}
}

func TestExecution_InterruptDuringStartIsDeliveredOnceAsSIGTERM(t *testing.T) {
	t.Parallel()
	fake, c := newFakeDaemon(t)
	c.onKill = func(signal string) error {
		if signal == "SIGTERM" {
			c.exitWith(143)
		}
		return nil
	}
	x, _ := newTestExecution(t, fake, func(c *Config) { c.CancelSignal = process.SIGKILL })
	start := fake.containerStart
	fake.containerStart = func(ctx context.Context, id string) error {
		_ = x.Interrupt()
		_ = x.Interrupt()
		return start(ctx, id)
	}

	if err := x.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_ = x.Interrupt()

	if got, want := strings.Join(fake.Calls(), ","), "ContainerCreate,ContainerAttach,ContainerWait,ContainerStart,ContainerKill SIGTERM"; got != want {
		t.Errorf("daemon calls = [%s], want [%s]", got, want)
	}
	if got, want := x.WaitStatus().ExitStatus(), 143; got != want {
		t.Errorf("WaitStatus().ExitStatus() = %d, want %d", got, want)
	}
}

func TestExecution_WaitFailureBeforeStartNeverStarts(t *testing.T) {
	t.Parallel()
	fake, _ := newFakeDaemon(t)
	fake.containerWait = func(context.Context, string) client.ContainerWaitResult {
		errC := make(chan error, 1)
		errC <- errors.New("wait not supported")
		return client.ContainerWaitResult{Error: errC}
	}
	x, _ := newTestExecution(t, fake, nil)

	if err := x.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "wait not supported") {
		t.Fatalf("Run() error = %v, want the wait failure", err)
	}
	if calls := fake.Calls(); slices.Contains(calls, "ContainerStart") {
		t.Errorf("daemon calls = %v, want no ContainerStart", calls)
	}
}

func TestExecution_TerminateKillsARunningContainer(t *testing.T) {
	t.Parallel()
	fake, _ := newFakeDaemon(t)
	x, _ := newTestExecution(t, fake, nil)

	wait := runInBackground(t, x)
	waitForState(t, x, stateRunning)
	if err := x.Terminate(); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if err := wait(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := x.WaitStatus().ExitStatus(), 137; got != want {
		t.Errorf("WaitStatus().ExitStatus() = %d, want %d", got, want)
	}
	if calls := fake.Calls(); calls[len(calls)-1] != "ContainerKill SIGKILL" {
		t.Errorf("last daemon call = %q, want ContainerKill SIGKILL", calls[len(calls)-1])
	}
}

func TestExecution_TerminateDuringStartKillsOnceStarted(t *testing.T) {
	t.Parallel()
	fake, _ := newFakeDaemon(t)
	x, _ := newTestExecution(t, fake, nil)
	start := fake.containerStart
	fake.containerStart = func(ctx context.Context, id string) error {
		if err := x.Terminate(); err != nil {
			t.Errorf("Terminate() error = %v", err)
		}
		return start(ctx, id)
	}

	if err := x.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := x.WaitStatus().ExitStatus(), 137; got != want {
		t.Errorf("WaitStatus().ExitStatus() = %d, want %d", got, want)
	}
}

func TestExecution_TerminateUnblocksAHungDaemonBeforeStart(t *testing.T) {
	t.Parallel()
	fake, _ := newFakeDaemon(t)
	attaching := make(chan struct{})
	fake.containerAttach = func(ctx context.Context, _ string) (client.ContainerAttachResult, error) {
		close(attaching)
		<-ctx.Done()
		return client.ContainerAttachResult{}, ctx.Err()
	}
	x, _ := newTestExecution(t, fake, nil)

	wait := runInBackground(t, x)
	<-attaching
	if err := x.Terminate(); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if err := wait(); err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if err := x.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if got, want := strings.Join(fake.Calls(), ","), "ContainerCreate,ContainerAttach,ContainerRemove cid"; got != want {
		t.Errorf("daemon calls = [%s], want [%s]", got, want)
	}
}

func TestExecution_FailedKillsStillLetRunReturn(t *testing.T) {
	t.Parallel()
	fake, c := newFakeDaemon(t)
	c.onKill = func(string) error { return errors.New("daemon not responding") }
	x, _ := newTestExecution(t, fake, nil)

	wait := runInBackground(t, x)
	waitForState(t, x, stateRunning)
	if err := x.Interrupt(); err != nil {
		t.Errorf("Interrupt() error = %v, want nil so the caller goes on to Terminate", err)
	}
	if err := x.Terminate(); err == nil {
		t.Error("Terminate() error = nil, want the kill failure")
	}
	if err := wait(); err == nil || !strings.Contains(err.Error(), "stopped waiting for the job container") {
		t.Fatalf("Run() error = %v, want it to stop waiting", err)
	}
}

func TestExecution_WaitErrorFallsBackToInspect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		state     container.State
		wantCode  int
		wantErr   string
		wantCalls string
	}{
		{
			name:      "exited",
			state:     container.State{Status: container.StateExited, ExitCode: 7},
			wantCode:  7,
			wantCalls: "ContainerCreate,ContainerAttach,ContainerWait,ContainerStart,ContainerInspect",
		},
		{
			name:      "still_running",
			state:     container.State{Status: container.StateRunning, Running: true},
			wantErr:   "lost track of the job container",
			wantCalls: "ContainerCreate,ContainerAttach,ContainerWait,ContainerStart,ContainerInspect,ContainerKill SIGKILL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake, c := newFakeDaemon(t)
			fake.containerWait = func(context.Context, string) client.ContainerWaitResult {
				errC := make(chan error, 1)
				go func() {
					<-c.written // The connection drops once the container is running.
					errC <- errors.New("connection reset")
				}()
				return client.ContainerWaitResult{Error: errC}
			}
			fake.containerInspect = func(string) (client.ContainerInspectResult, error) {
				// A dropped daemon connection ends the attach stream too.
				<-c.written
				_ = c.server.Close()
				return client.ContainerInspectResult{Container: container.InspectResponse{State: &tc.state}}, nil
			}
			x, _ := newTestExecution(t, fake, nil)

			err := x.Run(t.Context())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Run() error = %v, want an error containing %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := x.WaitStatus().ExitStatus(); err == nil && got != tc.wantCode {
				t.Errorf("WaitStatus().ExitStatus() = %d, want %d", got, tc.wantCode)
			}
			if got := strings.Join(fake.Calls(), ","); got != tc.wantCalls {
				t.Errorf("daemon calls = [%s], want [%s]", got, tc.wantCalls)
			}
		})
	}
}

func TestExecution_Cleanup(t *testing.T) {
	t.Parallel()

	t.Run("never_run", func(t *testing.T) {
		t.Parallel()
		fake, _ := newFakeDaemon(t)
		x, _ := newTestExecution(t, fake, nil)
		if err := x.Cleanup(t.Context()); err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
		if calls := fake.Calls(); len(calls) != 0 {
			t.Errorf("daemon calls = %v, want none", calls)
		}
	})

	for name, removeErr := range map[string]error{
		"already_removed": cerrdefs.ErrNotFound,
		"remove_fails":    errors.New("daemon not responding"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, c := newFakeDaemon(t)
			fake.containerRemove = func(string, client.ContainerRemoveOptions) error { return removeErr }
			x, _ := newTestExecution(t, fake, nil)
			start := fake.containerStart
			fake.containerStart = func(ctx context.Context, id string) error {
				err := start(ctx, id)
				c.exitWith(0)
				return err
			}
			if err := x.Run(t.Context()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			err := x.Cleanup(t.Context())
			if wantErr := !cerrdefs.IsNotFound(removeErr); (err != nil) != wantErr {
				t.Errorf("Cleanup() error = %v, want error: %t", err, wantErr)
			}
		})
	}
}

// waitForState waits until x reaches state.
func waitForState(t *testing.T, x *Execution, state executionState) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		x.mu.Lock()
		got := x.state
		x.mu.Unlock()
		if got == state {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("execution state = %d, want %d within 10s", got, state)
		}
	}
}

// stdFrame encodes payload as one frame of Docker's multiplexed stream.
func stdFrame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}
