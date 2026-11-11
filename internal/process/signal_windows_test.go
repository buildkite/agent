//go:build windows

package process_test

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	"golang.org/x/sys/windows"
)

const windowsStillActiveExitCode = 259

var (
	testKernel32           = syscall.NewLazyDLL("kernel32.dll")
	testProcGetHandleCount = testKernel32.NewProc("GetProcessHandleCount")
)

func init() {
	extraTestMainCases["windows-background-child"] = func() {
		grandchild := exec.Command(os.Args[0])
		grandchild.Env = append(os.Environ(), "TEST_MAIN=windows-sleep")
		if err := grandchild.Start(); err != nil {
			log.Fatalf("start grandchild: %v", err)
		}
		fmt.Printf("%d\n", grandchild.Process.Pid)
		os.Exit(0)
	}

	extraTestMainCases["windows-sleep"] = func() {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
}

func currentProcessHandleCount(t *testing.T) uint32 {
	t.Helper()
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	runtime.GC()

	var count uint32
	r1, _, err := testProcGetHandleCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
	if r1 == 0 {
		t.Fatalf("GetProcessHandleCount failed: %v", err)
	}
	return count
}

func TestWindowsJobHandleReleasedOnCleanExit(t *testing.T) {
	t.Parallel()

	p := process.New(logger.Discard, process.Config{
		Path: os.Args[0],
		Env:  []string{"TEST_MAIN=output", "GOCOVERDIR=" + t.TempDir()},
	})
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}
	if got := p.WinJobHandle(); got != 0 {
		t.Fatalf("p.WinJobHandle() = %#x, want 0", got)
	}
	if err := p.Terminate(); err != nil {
		t.Fatalf("p.Terminate() after clean exit = %v", err)
	}
}

func TestWindowsJobHandleReleasedOnStartFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		cfg  process.Config
	}{
		{
			name: "missing working directory",
			cfg: process.Config{
				Path: os.Args[0],
				Dir:  filepath.Join(t.TempDir(), "does-not-exist"),
			},
		},
		{
			name: "missing executable",
			cfg: process.Config{
				Path: filepath.Join(t.TempDir(), "missing-binary.exe"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := process.New(logger.Discard, tc.cfg)
			if err := p.Run(t.Context()); err == nil {
				t.Fatalf("p.Run() succeeded, want error")
			}
			if got := p.WinJobHandle(); got != 0 {
				t.Fatalf("p.WinJobHandle() = %#x, want 0", got)
			}
		})
	}
}

func TestWindowsCleanExitPreservesBackgroundChild(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	coverDir := t.TempDir()
	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=windows-background-child", "GOCOVERDIR=" + coverDir},
		Stdout: stdout,
	})
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}
	if got := p.WinJobHandle(); got != 0 {
		t.Fatalf("p.WinJobHandle() = %#x, want 0", got)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		t.Fatalf("strconv.Atoi(%q) = %v", stdout.String(), err)
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		t.Fatalf("OpenProcess(%d) = %v (background child exited unexpectedly)", pid, err)
	}
	t.Cleanup(func() {
		_ = windows.TerminateProcess(h, 0)
		_ = windows.CloseHandle(h)
	})

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		t.Fatalf("GetExitCodeProcess(%d) = %v", pid, err)
	}
	if exitCode != windowsStillActiveExitCode {
		t.Fatalf("background child %d exitCode = %d, want %d (STILL_ACTIVE)", pid, exitCode, windowsStillActiveExitCode)
	}
}

func TestWindowsJobHandleDoesNotLeakAcrossRuns(t *testing.T) {
	coverDir := t.TempDir()
	runOnce := func() {
		p := process.New(logger.Discard, process.Config{
			Path: os.Args[0],
			Env:  []string{"TEST_MAIN=output", "GOCOVERDIR=" + coverDir},
		})
		if err := p.Run(t.Context()); err != nil {
			t.Fatalf("p.Run() = %v", err)
		}
	}

	for range 5 {
		runOnce()
	}

	const runs = 50
	before := currentProcessHandleCount(t)
	for range runs {
		runOnce()
	}
	after := currentProcessHandleCount(t)

	delta := int64(after) - int64(before)
	t.Logf("handles before=%d after=%d delta=%d (runs=%d)", before, after, delta, runs)
	if delta >= runs {
		t.Fatalf("leaked handles across %d runs: before=%d after=%d delta=%d", runs, before, after, delta)
	}
}
