//go:build unix

package vmsandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// detachedProc puts the child in its own session so that signals aimed at
// the agent's process group (Ctrl-C, systemd stop) don't reach Firecracker
// directly. Firecracker's lifetime is managed explicitly by the Runner (and
// by Sweep after a crash), never by incidental signal delivery.
func detachedProc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// killAndWait sends SIGKILL to pid and waits until it's gone. It's only used
// for processes that aren't our children (we can't Wait on those), so
// "gone" is detected by kill(pid, 0) failing with ESRCH. A zombie left by a
// dead parent will eventually be reaped by init; we treat one as gone too.
func killAndWait(pid int, timeout time.Duration) error {
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("pid %d still present after %v", pid, timeout)
}

// processRunning reports whether pid is alive and not a zombie, using
// /proc. Without /proc (non-Linux) it can't tell, and says "yes" so the
// caller keeps waiting rather than declaring victory early.
func processRunning(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		if os.IsNotExist(err) {
			_, procErr := os.Stat("/proc/self")
			return procErr != nil // no /proc at all: unknown, assume running
		}
		return true
	}
	// "pid (comm) S ..." — comm may contain spaces/parens, so find the last ')'.
	if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) {
		return stat[i+2] != 'Z'
	}
	return true
}
