//go:build darwin

package cryptominer

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"

	"golang.org/x/sys/unix"
)

// ScanProcessGroupPortable uses macOS sysctl to scan for miners in a process group.
func ScanProcessGroupPortable(_ context.Context, pgid int) (ScanResult, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return ScanResult{}, err
	}

	var processes []ProcessInfo
	for _, kp := range procs {
		info := ProcessInfo{
			PID:  int(kp.Proc.P_pid),
			PGID: int(kp.Eproc.Pgid),
			Comm: unix.ByteSliceToString(kp.Proc.P_comm[:]),
		}

		if exePath, cmdline, err := procArgs(info.PID); err == nil {
			info.ExePath = exePath
			info.Cmdline = cmdline
		}

		processes = append(processes, info)
	}

	return scanProcesses(processes), nil
}

// procArgs reads the executable path and full command line for a process
// using sysctl kern.procargs2. The returned format from the kernel is:
//
//	int32(argc) | exec_path\0 | padding\0... | arg0\0 | arg1\0 | ...
func procArgs(pid int) (exePath, cmdline string, err error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", "", err
	}

	if len(data) < 4 {
		return "", "", unix.EINVAL
	}

	argc := int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]

	// Executable path is the first null-terminated string.
	nullIdx := bytes.IndexByte(data, 0)
	if nullIdx == -1 {
		return "", "", unix.EINVAL
	}
	exePath = string(data[:nullIdx])
	data = data[nullIdx:]

	// Skip padding null bytes between exec path and argv.
	for len(data) > 0 && data[0] == 0 {
		data = data[1:]
	}

	// Read argc argument strings.
	var args []string
	for i := 0; i < argc && len(data) > 0; i++ {
		nullIdx = bytes.IndexByte(data, 0)
		if nullIdx == -1 {
			args = append(args, string(data))
			break
		}
		args = append(args, string(data[:nullIdx]))
		data = data[nullIdx+1:]
	}

	return exePath, strings.Join(args, " "), nil
}
