package dockerbootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

// Clienter separates lifecycle decisions from Docker subprocess execution.
// Run returns a process exit code; errors indicate failure to execute or a
// cancelled operation. Callers decide which commands return container status.
type Clienter interface {
	Run(context.Context, []string, map[string]string, io.Writer, io.Writer) (int, error)
}

// CLI deliberately ignores inherited Docker configuration and environment.
// The supervisor inherits job variables, including potentially DOCKER_HOST and
// DOCKER_CONFIG. Only operator-supplied arguments may select these settings.
type CLI struct {
	Path      string
	ConfigDir string
}

func (c CLI) Run(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) (int, error) {
	if !filepath.IsAbs(c.Path) || !filepath.IsAbs(c.ConfigDir) {
		return 0, fmt.Errorf("docker executable and configuration paths must be absolute")
	}
	argv := append([]string{"--host", "unix:///var/run/docker.sock", "--config", c.ConfigDir}, args...)
	cmd := exec.CommandContext(ctx, c.Path, argv...)
	// A minimal environment also avoids loading host credentials or job-selected
	// Docker helpers. --env NAME reads the selected container values from here.
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	isolateProcess(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}
