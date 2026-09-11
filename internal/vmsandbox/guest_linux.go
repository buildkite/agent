//go:build linux

package vmsandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	"golang.org/x/sys/unix"
)

// hostCID is the well-known vsock address of the host.
const hostCID = 2

// GuestConfig configures RunGuest.
type GuestConfig struct {
	// ConnectTimeout bounds how long the helper keeps retrying the vsock
	// connection to the host.
	ConnectTimeout time.Duration

	// PowerOff is called when the job is over (or the host has gone away).
	// It's a parameter so tests can run the helper without turning off the
	// machine they're running on.
	PowerOff func()
}

// RunGuest is the guest side of the sandbox: the body of `buildkite-agent
// vm-guest-bootstrap`. It connects to the host, receives the job, runs
// `buildkite-agent bootstrap`, streams output, reports the exit status and
// powers the guest off.
//
// It never returns to the caller with the guest still running: every path
// ends in conf.PowerOff. Losing the host connection at any point is treated
// as "the host is gone; stop", which is what makes an abrupt agent crash
// safe: the guest turns itself off rather than running on unsupervised.
func RunGuest(ctx context.Context, l logger.Logger, conf GuestConfig) error {
	defer conf.PowerOff()

	if conf.ConnectTimeout <= 0 {
		conf.ConnectTimeout = 30 * time.Second
	}
	rw, err := connectHost(conf.ConnectTimeout)
	if err != nil {
		return fmt.Errorf("connecting to host: %w", err)
	}
	defer rw.Close()
	conn := NewConn(rw)

	if err := conn.Send(Message{Type: TypeReady}); err != nil {
		return fmt.Errorf("sending ready: %w", err)
	}
	start, err := conn.Recv()
	if err != nil {
		return fmt.Errorf("waiting for start message: %w", err)
	}
	if start.Type != TypeStart {
		return fmt.Errorf("expected start message, got %q", start.Type)
	}

	exit := runJob(ctx, l, conn, start)
	if err := conn.Send(exit); err != nil {
		l.Errorf("vm-guest-bootstrap: sending exit status: %v", err)
	}

	// Wait for the host to tell us to power off (or to go away). The host
	// waits for Firecracker to exit before removing our disk, so this is
	// also what keeps the exit message from being lost in a race.
	for {
		m, err := conn.Recv()
		if err != nil || m.Type == TypePowerOff {
			return nil
		}
	}
}

// connectHost dials the host's vsock port, retrying until timeout. The
// runner listens before launching Firecracker, so this normally succeeds
// first time; retries cover the guest's own network stack coming up.
func connectHost(timeout time.Duration) (*os.File, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		err = unix.Connect(fd, &unix.SockaddrVM{CID: hostCID, Port: GuestPort})
		if err == nil {
			return os.NewFile(uintptr(fd), "vsock"), nil
		}
		unix.Close(fd)
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// runJob writes the job context files, runs bootstrap with its output going
// to conn, handles interrupt messages, and returns the exit message.
func runJob(ctx context.Context, l logger.Logger, conn *Conn, start Message) Message {
	out := outputWriter{conn}
	fail := func(format string, args ...any) Message {
		msg := fmt.Sprintf(format, args...)
		l.Errorf("vm-guest-bootstrap: %s", msg)
		_, _ = fmt.Fprintf(out, "[vm-sandbox] %s\n", msg)
		return Message{Type: TypeExit, ExitStatus: -1}
	}

	// The host sends only the env the job runner computed. Layer it over our
	// own process env (PATH, HOME, etc from init), with the host's values
	// winning except where the guest's own value is the meaningful one.
	environ := env.FromSlice(os.Environ())
	for name, val := range env.SeqSlice(start.Env) {
		if _, keep := guestEnvPriority[name]; keep {
			continue
		}
		environ.Set(name, val)
	}

	// Recreate the job context files under the guest context dir. GuestEnv
	// already rewrote the path vars to point there.
	if err := os.MkdirAll(GuestContextDir, 0o755); err != nil {
		return fail("creating %s: %v", GuestContextDir, err)
	}
	for name, content := range map[string]string{
		"BUILDKITE_ENV_FILE":      start.EnvFile,
		"BUILDKITE_ENV_JSON_FILE": start.EnvJSONFile,
	} {
		path, has := environ.Get(name)
		if !has || path == "" {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fail("writing %s: %v", path, err)
		}
	}
	for _, dir := range []string{GuestBuildPath, GuestPluginsPath, GuestSocketsPath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail("creating %s: %v", dir, err)
		}
	}

	self, err := os.Executable()
	if err != nil {
		return fail("finding own executable: %v", err)
	}
	environ.Set("BUILDKITE_BIN_PATH", filepath.Dir(self))
	environ.Set("BUILDKITE_AGENT_PID", fmt.Sprint(os.Getpid()))

	cancelSignal := process.SIGTERM
	if sig, has := environ.Get("BUILDKITE_CANCEL_SIGNAL"); has {
		cs, err := process.ParseSignal(sig)
		if err != nil {
			return fail("parsing BUILDKITE_CANCEL_SIGNAL: %v", err)
		}
		// As in kubernetes-bootstrap: SIGKILL is for the command, not for
		// bootstrap itself, which needs to run its cleanup hooks.
		if cs == process.SIGKILL {
			cs = process.SIGTERM
		}
		cancelSignal = cs
	}
	signalGrace := 10 * time.Second
	if s, has := environ.Get("BUILDKITE_CANCEL_SIGNAL_TIMEOUT"); has {
		if d, err := time.ParseDuration(s); err == nil {
			signalGrace = d
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	proc := process.New(l, process.Config{
		Path:              self,
		Args:              []string{"bootstrap"},
		Env:               environ.ToSlice(),
		Stdout:            out,
		Stderr:            out,
		Dir:               GuestBuildPath,
		PTY:               environ.GetBool("BUILDKITE_PTY", true),
		InterruptSignal:   cancelSignal,
		SignalGracePeriod: signalGrace,
	})

	// Host messages during the job: interrupt, or the host going away.
	timeoutFile, _ := environ.Get("BUILDKITE_AGENT_JOB_TIMEOUT_FILE")
	go func() {
		for {
			m, err := conn.Recv()
			if err != nil {
				l.Warnf("vm-guest-bootstrap: lost host connection (%v); cancelling job", err)
				cancel()
				return
			}
			switch m.Type {
			case TypeInterrupt:
				if m.TimedOut && timeoutFile != "" {
					if err := os.WriteFile(timeoutFile, []byte("job_timeout\n"), 0o600); err != nil {
						l.Warnf("vm-guest-bootstrap: writing timeout marker: %v", err)
					}
				}
				// process.Run turns this into Interrupt, then Terminate after
				// SignalGracePeriod.
				cancel()
			case TypePowerOff:
				// Early power-off while the job is running: the host has
				// given up on us. Cancel, and RunGuest's deferred PowerOff
				// does the rest once bootstrap is gone.
				cancel()
				return
			}
		}
	}()

	if err := proc.Run(ctx); err != nil {
		return fail("couldn't execute bootstrap: %v", err)
	}
	ws := proc.WaitStatus()
	m := Message{Type: TypeExit, ExitStatus: ws.ExitStatus(), Signaled: ws.Signaled()}
	if ws.Signaled() {
		m.Signal = int(ws.Signal())
	}
	return m
}

// guestEnvPriority lists env vars where the guest process's own value wins
// over the host-computed one. The rest of the host env is authoritative.
var guestEnvPriority = map[string]struct{}{
	"HOME":     {},
	"HOSTNAME": {},
	"PATH":     {},
	"PWD":      {},
	"SHLVL":    {},
	"TERM":     {},
	"USER":     {},
	"LOGNAME":  {},
}

// PowerOffGuest syncs filesystems and powers the machine off. It's the
// default GuestConfig.PowerOff. It doesn't return.
func PowerOffGuest() {
	unix.Sync()
	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		// Not root, or not PID 1's child in a sandbox that allows it. The
		// host will kill Firecracker after its shutdown timeout anyway.
		_, _ = fmt.Fprintf(os.Stderr, "vm-guest-bootstrap: power off failed: %v\n", err)
		os.Exit(1)
	}
}
