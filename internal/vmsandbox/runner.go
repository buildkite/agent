package vmsandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
)

// RunnerConfig is the per-job input to a Runner.
type RunnerConfig struct {
	// JobID names the per-job state directory.
	JobID string

	// Env is the bootstrap environment computed by the job runner (job env
	// plus agent settings). It is rewritten with GuestEnv before being sent.
	// It must not include the agent process's own environment.
	Env []string

	// EnvFile and EnvJSONFile are host paths of the job env files the agent
	// created. Their contents are sent to the guest, which recreates them.
	EnvFile     string
	EnvJSONFile string

	// JobTimeoutFile is the host path of the job timeout marker. The agent
	// creates it just before calling Interrupt on a job-level timeout, so
	// Interrupt checks for it and tells the guest to create its own copy.
	JobTimeoutFile string

	// Stdout receives bootstrap output and sandbox diagnostics. (bootstrap's
	// stdout and stderr are merged on the guest, as they are when run in a
	// PTY locally.)
	Stdout io.Writer

	// SignalGracePeriod is how long Run waits after a context cancellation
	// has been forwarded as an interrupt before terminating the guest.
	SignalGracePeriod time.Duration
}

// Runner runs one job in one disposable microVM. It satisfies the agent's
// jobProcess interface.
type Runner struct {
	sandbox *Sandbox
	conf    RunnerConfig
	dir     string

	started chan struct{}
	done    chan struct{}

	mu       sync.Mutex
	conn     *Conn     // nil until the guest connects
	fc       *exec.Cmd // nil until Firecracker is started
	fcExited chan struct{}
	fcErr    error
	status   waitStatus
	statusOK bool // an exit message was received
	killed   bool // Terminate was called
}

// NewRunner prepares a Runner. Nothing happens until Run.
func (s *Sandbox) NewRunner(conf RunnerConfig) *Runner {
	if conf.SignalGracePeriod <= 0 {
		conf.SignalGracePeriod = 10 * time.Second
	}
	return &Runner{
		sandbox:  s,
		conf:     conf,
		dir:      filepath.Join(s.conf.stateDir(), conf.JobID),
		started:  make(chan struct{}),
		done:     make(chan struct{}),
		fcExited: make(chan struct{}),
	}
}

// Started is closed once Firecracker has been launched.
func (r *Runner) Started() <-chan struct{} { return r.started }

// Done is closed once the job has finished and the sandbox has been torn
// down (or teardown has failed and the sandbox has been poisoned).
func (r *Runner) Done() <-chan struct{} { return r.done }

// WaitStatus returns the bootstrap exit status as reported by the guest. If
// the guest never reported one, the status is -1, and Signaled/SIGKILL if
// that was because the host killed the VM.
func (r *Runner) WaitStatus() process.WaitStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.statusOK {
		return r.status
	}
	if r.killed {
		return waitStatus{code: -1, signaled: true, signal: syscall.SIGKILL}
	}
	return waitStatus{code: -1}
}

// Run boots the guest, runs bootstrap in it, and tears the guest down. It
// returns an error only if the job never started (no guest readiness); once
// bootstrap has run, its result is reported through WaitStatus even if
// teardown then fails.
func (r *Runner) Run(ctx context.Context) (err error) {
	defer close(r.done)

	if err := r.sandbox.checkPoison(); err != nil {
		return err
	}

	if err := r.prepare(); err != nil {
		_ = os.RemoveAll(r.dir)
		return fmt.Errorf("preparing sandbox: %w", err)
	}

	// Listen for the guest before starting it, so there's no window where
	// the guest can connect to nothing.
	ln, err := net.Listen("unix", r.vsockHostPath())
	if err != nil {
		_ = os.RemoveAll(r.dir)
		return fmt.Errorf("listening for guest: %w", err)
	}
	defer func() { _ = ln.Close() }()

	if err := r.startFirecracker(); err != nil {
		_ = os.RemoveAll(r.dir)
		return fmt.Errorf("starting firecracker: %w", err)
	}
	close(r.started)

	// From here on the guest exists and must be torn down whatever happens.
	defer r.teardown()

	conn, err := r.awaitGuest(ctx, ln)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := r.sendStart(); err != nil {
		return fmt.Errorf("sending job to guest: %w", err)
	}

	// Forward context cancellation the way process.Process does: interrupt,
	// then terminate after the grace period.
	loopDone := make(chan struct{})
	defer close(loopDone)
	go func() {
		select {
		case <-loopDone:
			return
		case <-ctx.Done():
		}
		_ = r.Interrupt()
		select {
		case <-loopDone:
		case <-time.After(r.conf.SignalGracePeriod):
			_ = r.Terminate()
		}
	}()

	r.readLoop()
	return nil
}

// prepare creates the per-job directory with a fresh copy of the root
// filesystem and the Firecracker config.
func (r *Runner) prepare() error {
	if err := os.Mkdir(r.dir, 0o755); err != nil {
		return err
	}
	// A full per-job copy of the base image. `cp --sparse=always` keeps the
	// unwritten parts of the ext4 image as holes, so this costs roughly the
	// used size of the image, not its nominal size.
	cp := exec.Command("cp", "--sparse=always", "--reflink=auto", r.sandbox.conf.rootfsPath(), r.rootfsPath())
	if out, err := cp.CombinedOutput(); err != nil {
		return fmt.Errorf("copying rootfs: %w: %s", err, strings.TrimSpace(string(out)))
	}

	vmConfig, err := json.MarshalIndent(r.firecrackerConfig(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.dir, "vm.json"), vmConfig, 0o644)
}

func (r *Runner) rootfsPath() string    { return filepath.Join(r.dir, "rootfs.ext4") }
func (r *Runner) apiSockPath() string   { return filepath.Join(r.dir, "fc.sock") }
func (r *Runner) vsockPath() string     { return filepath.Join(r.dir, "vsock.sock") }
func (r *Runner) vsockHostPath() string { return r.vsockPath() + "_" + strconv.Itoa(GuestPort) }
func (r *Runner) pidfilePath() string   { return filepath.Join(r.dir, "firecracker.pid") }
func (r *Runner) consolePath() string   { return filepath.Join(r.dir, "console.log") }

// firecrackerConfig is the --config-file document. Only what this runner
// needs: one kernel, one root drive, one tap, one vsock.
func (r *Runner) firecrackerConfig() map[string]any {
	sc := r.sandbox.conf
	tap := sc.Tap
	netmask := net.CIDRMask(tap.HostAddr.Bits(), 32)
	bootArgs := strings.Join([]string{
		"console=ttyS0", "reboot=k", "panic=1",
		// Static IP via the kernel's own autoconfig (CONFIG_IP_PNP), so the
		// guest has networking before userspace starts.
		fmt.Sprintf("ip=%s::%s:%s::eth0:off", tap.GuestIP, tap.HostAddr.Addr(), net.IP(netmask)),
		"bko_dns=" + strings.Join(sc.DNS, ","),
		"init=/sbin/init",
	}, " ")
	return map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": sc.kernelPath(),
			"boot_args":         bootArgs,
		},
		"drives": []map[string]any{{
			"drive_id":       "rootfs",
			"path_on_host":   r.rootfsPath(),
			"is_root_device": true,
			"is_read_only":   false,
		}},
		"machine-config": map[string]any{
			"vcpu_count":   sc.VCPUs,
			"mem_size_mib": sc.MemoryMiB,
		},
		"network-interfaces": []map[string]any{{
			"iface_id":      "eth0",
			"guest_mac":     "AA:FC:00:00:00:01",
			"host_dev_name": tap.Device,
		}},
		"vsock": map[string]any{
			"guest_cid": 3,
			"uds_path":  r.vsockPath(),
		},
	}
}

func (r *Runner) startFirecracker() error {
	console, err := os.Create(r.consolePath())
	if err != nil {
		return err
	}
	defer func() { _ = console.Close() }()

	cmd := exec.Command(r.sandbox.conf.firecrackerPath(),
		"--api-sock", r.apiSockPath(),
		"--config-file", filepath.Join(r.dir, "vm.json"),
	)
	cmd.Stdout = console
	cmd.Stderr = console
	cmd.SysProcAttr = detachedProc()
	if err := cmd.Start(); err != nil {
		return err
	}
	// The pidfile is what Sweep uses after an agent crash. Write it before
	// anything else can go wrong.
	if err := os.WriteFile(r.pidfilePath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("writing pidfile: %w", err)
	}

	r.mu.Lock()
	r.fc = cmd
	r.mu.Unlock()

	go func() {
		r.fcErr = cmd.Wait()
		close(r.fcExited)
	}()
	r.sandbox.logger.Infof("[vmsandbox] Started firecracker pid=%d for job %s", cmd.Process.Pid, r.conf.JobID)
	return nil
}

// awaitGuest waits for the guest helper to connect and say it's ready.
func (r *Runner) awaitGuest(ctx context.Context, ln net.Listener) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		accepted <- result{c, err}
	}()

	var conn net.Conn
	select {
	case res := <-accepted:
		if res.err != nil {
			return nil, fmt.Errorf("accepting guest connection: %w", res.err)
		}
		conn = res.conn
	case <-r.fcExited:
		return nil, fmt.Errorf("firecracker exited before the guest connected (%v)%s", r.fcErr, r.consoleTail())
	case <-time.After(r.sandbox.conf.BootTimeout):
		return nil, fmt.Errorf("guest did not become ready within %v%s", r.sandbox.conf.BootTimeout, r.consoleTail())
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	c := NewConn(conn)
	_ = conn.SetReadDeadline(time.Now().Add(r.sandbox.conf.BootTimeout))
	m, err := c.Recv()
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("reading guest ready message: %w", err)
	}
	if m.Type != TypeReady {
		_ = conn.Close()
		return nil, fmt.Errorf("guest sent %q before ready", m.Type)
	}

	r.mu.Lock()
	r.conn = c
	r.mu.Unlock()
	r.sandbox.logger.Infof("[vmsandbox] Guest ready for job %s", r.conf.JobID)
	return conn, nil
}

func (r *Runner) sendStart() error {
	guestEnv, warnings := GuestEnv(r.conf.Env)
	for _, w := range warnings {
		r.sandbox.logger.Warnf("[vmsandbox] %s", w)
		_, _ = fmt.Fprintf(r.conf.Stdout, "[vm-sandbox] warning: %s\n", w)
	}
	m := Message{Type: TypeStart, Env: guestEnv}
	if r.conf.EnvFile != "" {
		b, err := os.ReadFile(r.conf.EnvFile)
		if err != nil {
			return err
		}
		m.EnvFile = string(b)
	}
	if r.conf.EnvJSONFile != "" {
		b, err := os.ReadFile(r.conf.EnvJSONFile)
		if err != nil {
			return err
		}
		m.EnvJSONFile = string(b)
	}
	return r.conn.Send(m)
}

// readLoop consumes guest messages until the exit report or the connection
// drops.
func (r *Runner) readLoop() {
	for {
		m, err := r.conn.Recv()
		if err != nil {
			r.mu.Lock()
			killed := r.killed
			r.mu.Unlock()
			if !killed {
				r.sandbox.logger.Warnf("[vmsandbox] Lost connection to guest for job %s before it reported an exit status: %v", r.conf.JobID, err)
				_, _ = fmt.Fprint(r.conf.Stdout, "+++ Unknown sandbox exit status\nThe sandbox guest stopped communicating without reporting an exit status. Perhaps the guest kernel panicked or ran out of memory?\n")
			}
			return
		}
		switch m.Type {
		case TypeOutput:
			_, _ = r.conf.Stdout.Write(m.Data)
		case TypeLog:
			r.sandbox.logger.Infof("[vmsandbox] guest: %s", m.Text)
			_, _ = fmt.Fprintf(r.conf.Stdout, "[vm-sandbox] %s\n", m.Text)
		case TypeExit:
			r.mu.Lock()
			r.status = waitStatus{code: m.ExitStatus, signaled: m.Signaled, signal: syscall.Signal(m.Signal)}
			r.statusOK = true
			r.mu.Unlock()
			return
		default:
			r.sandbox.logger.Warnf("[vmsandbox] Ignoring unexpected %q message from guest", m.Type)
		}
	}
}

// Interrupt asks the guest to send bootstrap its cancel signal. If the agent
// has written the job timeout marker, the guest is told to create its own.
func (r *Runner) Interrupt() error {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn == nil {
		// Not connected yet. The context cancellation path in Run will
		// terminate after the grace period; there's nothing to interrupt.
		return nil
	}
	timedOut := false
	if r.conf.JobTimeoutFile != "" {
		_, err := os.Stat(r.conf.JobTimeoutFile)
		timedOut = err == nil
	}
	return conn.Send(Message{Type: TypeInterrupt, TimedOut: timedOut})
}

// Terminate is the host-enforced stop: it kills Firecracker, which takes
// the guest and everything in it with it. It doesn't need the guest's
// cooperation.
func (r *Runner) Terminate() error {
	r.mu.Lock()
	fc := r.fc
	r.killed = true
	r.mu.Unlock()
	if fc == nil {
		return nil
	}
	r.sandbox.logger.Warnf("[vmsandbox] Killing firecracker pid=%d for job %s", fc.Process.Pid, r.conf.JobID)
	if err := fc.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// teardown stops the guest and removes the per-job directory. It's the
// gate on slot reuse: any failure poisons the sandbox.
func (r *Runner) teardown() {
	timeout := r.sandbox.conf.ShutdownTimeout

	// 1. Cooperative: ask the guest to power off. Firecracker exits when the
	//    guest does.
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		_ = conn.Send(Message{Type: TypePowerOff})
	}
	select {
	case <-r.fcExited:
	case <-time.After(timeout):
		// 2. Forced: kill the VMM.
		r.sandbox.logger.Warnf("[vmsandbox] Guest for job %s did not power off within %v; killing firecracker", r.conf.JobID, timeout)
		if err := r.Terminate(); err != nil {
			r.sandbox.poison(fmt.Sprintf("job %s: kill firecracker: %v", r.conf.JobID, err))
			return
		}
		select {
		case <-r.fcExited:
		case <-time.After(timeout):
			// 3. Confirm exit. SIGKILL not taking effect means something is
			//    badly wrong on the host (D state, or not our process).
			r.sandbox.poison(fmt.Sprintf("job %s: firecracker pid %d did not exit after SIGKILL", r.conf.JobID, r.fc.Process.Pid))
			return
		}
	}
	if r.fcErr != nil {
		r.sandbox.logger.Debugf("[vmsandbox] firecracker for job %s exited: %v", r.conf.JobID, r.fcErr)
	}

	// 4. Remove disposable resources.
	if err := os.RemoveAll(r.dir); err != nil {
		r.sandbox.poison(fmt.Sprintf("job %s: removing %s: %v", r.conf.JobID, r.dir, err))
		return
	}
	r.sandbox.logger.Infof("[vmsandbox] Sandbox for job %s torn down", r.conf.JobID)
}

// consoleTail returns the last part of the guest console log, for error
// messages when the guest never became ready.
func (r *Runner) consoleTail() string {
	b, err := os.ReadFile(r.consolePath())
	if err != nil || len(b) == 0 {
		return ""
	}
	const limit = 2048
	if len(b) > limit {
		b = b[len(b)-limit:]
	}
	return "\n--- guest console (tail) ---\n" + string(b)
}

// waitStatus is the guest-reported bootstrap exit status.
type waitStatus struct {
	code     int
	signaled bool
	signal   syscall.Signal
}

func (w waitStatus) ExitStatus() int        { return w.code }
func (w waitStatus) Signaled() bool         { return w.signaled }
func (w waitStatus) Signal() syscall.Signal { return w.signal }
