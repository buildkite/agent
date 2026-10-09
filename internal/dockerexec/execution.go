package dockerexec

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Time limits for daemon calls during a job. Attaching and waiting last as
// long as the job, so they have none.
const (
	createTimeout  = 60 * time.Second
	startTimeout   = 60 * time.Second
	controlTimeout = 10 * time.Second // kill, inspect, and remove
	drainTimeout   = 10 * time.Second // output still arriving after exit
)

// errCancelledBeforeStart is returned by Run when the job is cancelled
// before its container starts.
var errCancelledBeforeStart = errors.New("job was cancelled before its container started")

// Execution is one job running in a container. Run may be called once.
// Interrupt and Terminate may be called from other goroutines at any time.
// Cleanup is called after Run returns, or instead of Run.
type Execution struct {
	e    *Executor
	req  Request
	name string

	// interruptSignal is the cancel signal, with SIGKILL replaced by SIGTERM
	// so bootstrap can still run pre-exit hooks and report the exit status.
	interruptSignal process.Signal

	started chan struct{}
	done    chan struct{}

	mu              sync.Mutex
	state           executionState
	pending         pendingSignal
	interruptSent   bool
	createAttempted bool
	exited          bool // Run has finished waiting for the container.
	id              string
	cancelCreate    context.CancelFunc
	cancelStart     context.CancelFunc
	cancelRun       context.CancelFunc
	exitCode        int
}

// executionState tracks where Run is, so Interrupt and Terminate know
// whether there is a running container to signal.
type executionState int

const (
	stateNew executionState = iota
	stateCreating
	stateCreated
	stateStarting
	stateRunning
	stateFinished
)

// pendingSignal records the strongest cancellation requested so far. It only
// ever increases.
type pendingSignal int

const (
	signalNone pendingSignal = iota
	signalInterrupt
	signalTerminate
)

// NewExecution returns an execution for req. It makes no daemon calls, so
// nothing needs cleaning up until Run is called.
func (e *Executor) NewExecution(req Request) *Execution {
	sig := e.cfg.CancelSignal
	if sig == process.SIGKILL {
		sig = process.SIGTERM
	}
	return &Execution{
		e:               e,
		req:             req,
		name:            "buildkite-job-" + req.JobID + "-" + strings.ToLower(rand.Text()[:8]),
		interruptSignal: sig,
		started:         make(chan struct{}),
		done:            make(chan struct{}),
	}
}

// Started is closed when Run begins launching the container.
func (x *Execution) Started() <-chan struct{} { return x.started }

// Done is closed when Run returns.
func (x *Execution) Done() <-chan struct{} { return x.done }

// WaitStatus reports the container's exit code. Docker does not report
// which signal ended a container, so it is never Signaled.
func (x *Execution) WaitStatus() process.WaitStatus {
	x.mu.Lock()
	defer x.mu.Unlock()
	return exitStatus(x.exitCode)
}

// Run creates and starts the container and blocks until it exits. An error
// means there is no trustworthy exit code: the container failed to launch,
// or the agent lost track of it.
func (x *Execution) Run(ctx context.Context) error {
	close(x.started)
	defer close(x.done)

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	x.mu.Lock()
	x.cancelRun = cancelRun
	x.mu.Unlock()
	defer x.setState(stateFinished)

	if err := x.provisionMountSources(); err != nil {
		return err
	}
	id, err := x.create(runCtx)
	if err != nil {
		return err
	}

	// Attach and wait before starting, as the docker CLI does, so no early
	// output is lost and the exit cannot be missed however fast it is.
	attach, err := x.e.client.ContainerAttach(runCtx, id, client.ContainerAttachOptions{Stream: true, Stdout: true, Stderr: true})
	if err != nil {
		return fmt.Errorf("attaching to the job container: %w", err)
	}
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		x.copyOutput(attach.Reader)
	}()
	defer func() {
		attach.Close()
		<-copied
	}()
	wait := x.e.client.ContainerWait(runCtx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNextExit})
	waited := false
	defer func() {
		if !waited {
			// The client delivers a late result on an unbuffered channel, so
			// take it to let the client's goroutine finish.
			go func() {
				select {
				case <-wait.Result:
				case <-wait.Error:
				}
			}()
		}
	}()
	select {
	case err := <-wait.Error:
		waited = true
		return fmt.Errorf("waiting for the job container: %w", err)
	default:
	}

	if err := x.start(runCtx, id); err != nil {
		return err
	}

	code, err := x.waitForExit(runCtx, id, wait)
	waited = true
	x.mu.Lock()
	x.exited = true
	x.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-copied:
	case <-time.After(drainTimeout):
		x.e.logger.Warnf("Job container output did not finish within %v of the container exiting", drainTimeout)
	}
	x.mu.Lock()
	x.exitCode = code
	x.mu.Unlock()
	return nil
}

// Interrupt asks bootstrap to stop by sending it the cancel signal, once. If
// the container is not running yet, the request is recorded and Run acts on
// it. A failed kill is logged rather than returned, so the caller still goes
// on to Terminate.
func (x *Execution) Interrupt() error {
	x.mu.Lock()
	x.pending = max(x.pending, signalInterrupt)
	state, cancelCreate := x.state, x.cancelCreate
	x.mu.Unlock()

	switch {
	case state == stateCreating && cancelCreate != nil:
		cancelCreate()
	case state == stateRunning:
		x.deliverInterrupt()
	}
	return nil
}

// Terminate kills the container. Before start, it stops Run's daemon calls
// instead, and Cleanup removes whatever was created. During start, the
// daemon may already have launched the container while its response is
// delayed, so it kills the container and stops waiting for the response. If
// the daemon does not respond to the kill, it stops Run waiting on it, so the
// agent can move on even if the container is left running.
func (x *Execution) Terminate() error {
	x.mu.Lock()
	x.pending = signalTerminate
	state, id, cancelRun, cancelStart := x.state, x.id, x.cancelRun, x.cancelStart
	x.mu.Unlock()

	switch {
	case state == stateRunning:
		return x.terminateRunning(id)
	case state == stateStarting:
		cancelStart()
		err := x.kill(id, "SIGKILL")
		if err != nil {
			return fmt.Errorf("killing job container %s: %w", id, err)
		}
	case state < stateStarting && cancelRun != nil:
		cancelRun()
	}
	return nil
}

// Cleanup removes the container and its anonymous volumes. If create was
// abandoned before it returned an ID, the container is removed by its unique
// name in case the daemon created it anyway. A daemon that finishes creating
// it even later leaves a container that never started, which an operator can
// find by its labels.
func (x *Execution) Cleanup(ctx context.Context) error {
	x.mu.Lock()
	ref := x.id
	if ref == "" && x.createAttempted {
		ref = x.name
	}
	x.mu.Unlock()
	if ref == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	_, err := x.e.client.ContainerRemove(ctx, ref, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("removing job container %s: %w", ref, err)
	}
	return nil
}

// create creates the container, unless the job has already been cancelled.
// Interrupt and Terminate abandon a create in progress.
func (x *Execution) create(runCtx context.Context) (string, error) {
	createCtx, cancel := context.WithTimeout(runCtx, createTimeout)
	defer cancel()

	x.mu.Lock()
	if x.pending != signalNone {
		x.mu.Unlock()
		return "", errCancelledBeforeStart
	}
	x.state = stateCreating
	x.createAttempted = true
	x.cancelCreate = cancel
	x.mu.Unlock()

	opts := x.e.createOptions(x.req, x.name, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
	res, err := x.e.client.ContainerCreate(createCtx, opts)

	x.mu.Lock()
	x.cancelCreate = nil
	x.id = res.ID // Kept even if cancelled, for Cleanup.
	x.state = stateCreated
	pending := x.pending
	x.mu.Unlock()
	switch {
	case pending != signalNone:
		return "", errCancelledBeforeStart
	case cerrdefs.IsNotFound(err):
		return "", fmt.Errorf("creating the job container: %w (the docker executor image was removed after the agent started, restart the agent to pull it again)", err)
	case err != nil:
		return "", fmt.Errorf("creating the job container: %w", err)
	}
	for _, w := range res.Warnings {
		x.e.logger.Warnf("Docker warning creating job container: %s", w)
	}
	return res.ID, nil
}

// start starts the container, unless the job has been cancelled, then
// delivers any cancellation recorded while start was in progress.
func (x *Execution) start(runCtx context.Context, id string) error {
	x.mu.Lock()
	if x.pending != signalNone {
		x.mu.Unlock()
		return errCancelledBeforeStart
	}
	startCtx, cancel := context.WithTimeout(runCtx, startTimeout)
	defer cancel()
	x.state = stateStarting
	x.cancelStart = cancel
	x.mu.Unlock()

	_, err := x.e.client.ContainerStart(startCtx, id, client.ContainerStartOptions{})

	x.mu.Lock()
	pending := x.pending
	if err == nil && pending != signalTerminate {
		x.state = stateRunning
	}
	x.mu.Unlock()
	switch {
	case pending == signalTerminate:
		// Terminate may have tried to kill the container before the daemon
		// finished starting it, so kill it again. Cleanup removes it.
		if err == nil {
			if killErr := x.kill(id, "SIGKILL"); killErr != nil {
				x.e.logger.Errorf("Couldn't kill job container %s: %v", id, killErr)
			}
		}
		return errors.New("job was terminated while its container was starting")
	case err != nil:
		return fmt.Errorf("starting the job container: %w", err)
	}
	if pending == signalInterrupt {
		x.deliverInterrupt()
	}
	return nil
}

// waitForExit returns the container's exit code. A failed wait is not a job
// result: the container is inspected, and killed if it is still running.
func (x *Execution) waitForExit(runCtx context.Context, id string, wait client.ContainerWaitResult) (int, error) {
	var waitErr error
	select {
	case res := <-wait.Result:
		if res.Error == nil {
			return int(res.StatusCode), nil
		}
		waitErr = errors.New(res.Error.Message)
	case waitErr = <-wait.Error:
	}
	if runCtx.Err() != nil {
		return 0, fmt.Errorf("stopped waiting for the job container: %w", waitErr)
	}

	inspectCtx, cancel := context.WithTimeout(runCtx, controlTimeout)
	defer cancel()
	info, err := x.e.client.ContainerInspect(inspectCtx, id, client.ContainerInspectOptions{})
	if err == nil && info.Container.State != nil && !info.Container.State.Running &&
		(info.Container.State.Status == container.StateExited || info.Container.State.Status == container.StateDead) {
		return info.Container.State.ExitCode, nil
	}
	if killErr := x.kill(id, "SIGKILL"); killErr != nil {
		x.e.logger.Errorf("Couldn't kill job container %s: %v", id, killErr)
	}
	return 0, fmt.Errorf("lost track of the job container: %w", waitErr)
}

// deliverInterrupt sends the cancel signal to a running container, at most
// once. Bootstrap removes its signal handler after the first signal, so a
// second one would skip its cleanup.
func (x *Execution) deliverInterrupt() {
	x.mu.Lock()
	if x.interruptSent || x.state != stateRunning {
		x.mu.Unlock()
		return
	}
	x.interruptSent = true
	id := x.id
	x.mu.Unlock()

	if err := x.kill(id, x.interruptSignal.String()); err != nil {
		x.e.logger.Errorf("Couldn't send %s to job container %s: %v", x.interruptSignal, id, err)
	}
}

// terminateRunning kills a running container, and stops Run waiting on the
// daemon if the kill fails, or if Run is still waiting a while after a kill
// the daemon accepted.
func (x *Execution) terminateRunning(id string) error {
	x.mu.Lock()
	cancelRun := x.cancelRun
	x.mu.Unlock()

	if err := x.kill(id, "SIGKILL"); err != nil {
		cancelRun()
		return fmt.Errorf("killing job container %s: %w", id, err)
	}
	time.AfterFunc(controlTimeout, func() {
		x.mu.Lock()
		exited := x.exited
		x.mu.Unlock()
		if !exited {
			x.e.logger.Errorf("Job container %s did not exit within %v of SIGKILL", id, controlTimeout)
			cancelRun()
		}
	})
	return nil
}

// kill sends signal to the container. It has its own time limit so a hung
// daemon cannot wedge cancellation. A container that is no longer running
// is not an error: it has already stopped.
func (x *Execution) kill(id, signal string) error {
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	_, err := x.e.client.ContainerKill(ctx, id, client.ContainerKillOptions{Signal: signal})
	if cerrdefs.IsConflict(err) || cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// provisionMountSources creates the read-write directories the container
// mounts, as the agent user. Left to Docker, missing sources would be
// created as root, which would then break the next job.
func (x *Execution) provisionMountSources() error {
	for _, dir := range []string{x.e.cfg.BuildPath, x.e.cfg.PluginsPath, x.e.cfg.SocketsPath, x.e.cfg.JobContextDir, x.e.cfg.GitMirrorsPath} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s for the job container: %w", dir, err)
		}
	}
	return nil
}

// copyOutput copies the attach stream to the job output. Without a TTY the
// daemon multiplexes stdout and stderr, which go to the same writer.
func (x *Execution) copyOutput(r io.Reader) {
	var err error
	if x.e.cfg.RunInPty {
		_, err = io.Copy(x.req.Output, r)
	} else {
		_, err = stdcopy.StdCopy(x.req.Output, x.req.Output, r)
	}
	if err != nil {
		x.e.logger.Debugf("Job container output stream ended: %v", err)
	}
}

func (x *Execution) setState(s executionState) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.state = s
}

// exitStatus is a container exit code as a process.WaitStatus.
type exitStatus int

func (s exitStatus) ExitStatus() int      { return int(s) }
func (exitStatus) Signaled() bool         { return false }
func (exitStatus) Signal() syscall.Signal { return 0 }
