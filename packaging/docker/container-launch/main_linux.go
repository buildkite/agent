package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/buildkite/agent/v4/internal/registrationtoken"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

var startFlags string

func prepare(args []string, envFD string, flags []cli.Flag) ([]string, *os.File, error) {
	parsed := registrationtoken.ParseArgs(args, flags)
	token, found := parsed.Token()
	if !found {
		return args, nil, nil
	}
	protected, protectedErr := strconv.ParseUint(envFD, 10, 31)
	fds := make(map[int]bool)
	for _, option := range parsed.Options {
		if value, ok := strings.CutPrefix(option.Value, "fd://"); ok {
			fd, err := strconv.ParseUint(value, 10, 31)
			if err == nil && (protectedErr != nil || fd != protected) {
				fds[int(fd)] = true
			}
		}
	}
	defer func() {
		for fd := range fds {
			_ = unix.Close(fd)
		}
	}()
	if !parsed.Start || parsed.Help {
		return parsed.Replace(args, ""), nil, nil
	}
	if strings.HasPrefix(token, "file://") || token == "" {
		return parsed.Replace(args, token), nil, nil
	}
	if value, ok := strings.CutPrefix(token, "fd://"); ok {
		fd, err := strconv.ParseUint(value, 10, 31)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid token file descriptor: %w", err)
		}
		if protectedErr == nil && fd == protected {
			return parsed.Replace(args, "fd://"+envFD), nil, nil
		}
		f := os.NewFile(uintptr(fd), "container-argv-token")
		if f == nil {
			return nil, nil, fmt.Errorf("invalid token file descriptor")
		}
		b, err := io.ReadAll(io.LimitReader(f, 4097))
		_ = f.Close()
		delete(fds, int(fd))
		if err != nil {
			return nil, nil, fmt.Errorf("reading container argv token: %w", err)
		}
		if len(b) > 4096 {
			return nil, nil, fmt.Errorf("container argv token exceeds the 4096-byte limit")
		}
		token = strings.TrimSpace(string(b))
	}
	if token == "" {
		return parsed.Replace(args, token), nil, nil
	}
	if len(token) > 4096 {
		return nil, nil, fmt.Errorf("container argv token exceeds the 4096-byte limit")
	}
	for fd := range fds {
		_ = unix.Close(fd)
		delete(fds, fd)
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = w.Close() }()
	wfd := int(w.Fd())
	if err = unix.SetNonblock(wfd, true); err == nil {
		var n int
		n, err = unix.Write(wfd, []byte(token))
		if err == nil && n != len(token) {
			err = fmt.Errorf("token exceeds pipe capacity")
		}
	}
	if err == nil {
		_, err = unix.FcntlInt(r.Fd(), unix.F_SETFD, 0)
	}
	if err != nil {
		_ = r.Close()
		return nil, nil, fmt.Errorf("preparing container argv token pipe: %w", err)
	}
	return parsed.Replace(args, fmt.Sprintf("fd://%d", r.Fd())), r, nil
}

func run() error {
	flags, err := registrationtoken.DecodeFlags([]byte(startFlags))
	if err != nil {
		return err
	}
	args, pipe, err := prepare(os.Args[1:], os.Getenv("BUILDKITE_AGENT_CONTAINER_TOKEN_FD"), flags)
	if err != nil {
		return err
	}
	if pipe != nil {
		defer func() { _ = pipe.Close() }()
	}
	return syscall.Exec("/bin/bash", append([]string{"bash", "/usr/local/bin/buildkite-agent-entrypoint", "--buildkite-container-argv-ready"}, args...), os.Environ())
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
