//go:build windows

package vmsandbox

import (
	"errors"
	"syscall"
	"time"
)

// The sandbox never runs on Windows (New refuses non-Linux hosts); these
// exist so the package, and the agent that imports it, still compile there.

func detachedProc() *syscall.SysProcAttr { return nil }

func killAndWait(int, time.Duration) error {
	return errors.New("vm sandbox is not supported on Windows")
}
