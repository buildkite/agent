package clicommand

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

func init() {
	BuildkiteAgentCommands = append(BuildkiteAgentCommands, &cli.Command{
		Name: "internal-container-launch", Hidden: true, SkipFlagParsing: true,
		Description: "Prepare the container entrypoint before starting tini.",
		Action: func(_ context.Context, c *cli.Command) error {
			args := c.Args().Slice()
			environ := os.Environ()
			args, environ, pipe, err := containerRegistrationToken(args, environ)
			if err != nil {
				return err
			}
			if pipe != nil {
				defer func() { _ = pipe.Close() }()
			}
			return syscall.Exec("/sbin/tini", append([]string{"tini", "--", "/usr/local/bin/buildkite-agent-entrypoint", "--buildkite-container-init"}, args...), environ)
		},
	})
}

func containerRegistrationToken(args, environ []string) ([]string, []string, *os.File, error) {
	parsed := parseRegistrationTokenArgs(args)
	if !parsed.start {
		return args, environ, nil, nil
	}
	token := ""
	fds := make(map[int]bool)
	rememberFD := func(value string) {
		if strings.HasPrefix(value, "fd://") {
			if fd, err := strconv.Atoi(strings.TrimPrefix(value, "fd://")); err == nil && fd >= 0 {
				fds[fd] = true
			}
		}
	}
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, registrationTokenEnvVar+"="); ok {
			token = value
			rememberFD(value)
		}
	}
	for _, option := range parsed.options {
		rememberFD(option.value)
	}
	defer func() {
		for fd := range fds {
			_ = unix.Close(fd)
		}
	}()
	if parsed.help {
		return parsed.replace(args, ""), scrubTokenFromEnviron(environ), nil, nil
	}
	argToken, found := parsed.token()
	if found {
		token = argToken
	}
	if strings.HasPrefix(token, "fd://") {
		fd, _ := strconv.Atoi(strings.TrimPrefix(token, "fd://"))
		delete(fds, fd)
	}
	secret := token
	var err error
	if !strings.HasPrefix(token, "file://") {
		secret, err = resolveRegistrationToken(token)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolving container registration token: %w", err)
	}
	for fd := range fds {
		_ = unix.Close(fd)
		delete(fds, fd)
	}
	environ = scrubTokenFromEnviron(environ)
	if strings.HasPrefix(token, "file://") {
		if found {
			args = parsed.replace(args, token)
		} else {
			environ = append(environ, registrationTokenEnvVar+"="+token)
		}
		return args, environ, nil, nil
	}
	if secret == "" {
		return parsed.replace(args, ""), environ, nil, nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating container token pipe: %w", err)
	}
	defer func() { _ = w.Close() }()
	wfd := int(w.Fd())
	if err = unix.SetNonblock(wfd, true); err == nil {
		var n int
		n, err = unix.Write(wfd, []byte(secret))
		if err == nil && n != len(secret) {
			err = fmt.Errorf("token exceeds pipe capacity")
		}
	}
	if err == nil {
		_, err = unix.FcntlInt(r.Fd(), unix.F_SETFD, 0)
	}
	if err != nil {
		_ = r.Close()
		return nil, nil, nil, fmt.Errorf("preparing container token pipe: %w", err)
	}
	ref := fmt.Sprintf("fd://%d", r.Fd())
	if found {
		args = parsed.replace(args, ref)
	} else {
		environ = append(environ, registrationTokenEnvVar+"="+ref)
	}
	return args, environ, r, nil
}
