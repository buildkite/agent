package dockerbootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Clienter allows lifecycle tests to inject Docker failures.
// Run reports nonzero exits as codes; errors indicate execution or cancellation
// failures. The caller interprets whether the code belongs to Docker or the job.
type Clienter interface {
	Run(context.Context, []string, map[string]string, io.Writer, io.Writer) (int, error)
}

// CLI excludes inherited configuration because the supervisor receives untrusted
// job variables that could redirect Docker or select host credentials.
type CLI struct {
	Path              string
	ConfigDir         string
	AuthConfigDir     string
	HelperPath        string
	HelperEnvironment map[string]string
}

func (c CLI) Run(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) (int, error) {
	if !filepath.IsAbs(c.Path) || !filepath.IsAbs(c.ConfigDir) {
		return 0, fmt.Errorf("docker executable and configuration paths must be absolute")
	}
	configDir := c.ConfigDir
	path := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	isPull := len(args) > 0 && args[0] == "pull"
	if isPull && c.AuthConfigDir != "" {
		if !filepath.IsAbs(c.AuthConfigDir) {
			return 0, fmt.Errorf("docker authentication directory must be absolute")
		}
		configDir = c.AuthConfigDir
		if c.HelperPath != "" {
			path = c.HelperPath
		}
	}
	argv := append([]string{"--host", "unix:///var/run/docker.sock", "--config", configDir}, args...)
	cmd := exec.CommandContext(ctx, c.Path, argv...)
	// --env NAME requires container values in the client environment; prepare
	// rejects Docker control variables before they reach this boundary.
	cmd.Env = []string{"PATH=" + path}
	// Registry helpers receive operator credentials only during pulls, never jobs.
	if isPull && c.AuthConfigDir != "" {
		for name, value := range c.HelperEnvironment {
			if strings.HasPrefix(name, "DOCKER_") || name == "PATH" {
				return 0, fmt.Errorf("helper environment cannot override Docker settings or PATH")
			}
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
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
