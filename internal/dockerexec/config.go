// Package dockerexec runs job bootstrap inside an ephemeral Docker container,
// using the host agent's own binary and an operator-chosen image. The executor
// only supports Linux hosts, so all paths here are forward-slash paths.
package dockerexec

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/moby/moby/api/types/mount"
)

// Paths the executor owns inside every job container.
const (
	agentBinDir  = "/buildkite-agent/bin"
	agentBinPath = agentBinDir + "/buildkite-agent"
	homeDir      = "/tmp/buildkite-home"
)

// Config is the Docker executor's configuration, taken from the agent's.
type Config struct {
	// Image is the image whose filesystem and ENV jobs run in.
	Image string

	// Mounts are extra bind mounts, each "src:dst" or "src:dst:ro".
	Mounts []string

	// Env are extra container env entries, each "KEY=VALUE", or "KEY" to copy
	// the value from the agent's own environment.
	Env []string

	// Network is the container network mode. Empty means Docker's default.
	Network string

	// Host paths the executor mounts at the same path in the container.
	// GitMirrorsPath and SigningJWKSFile are optional.
	BuildPath       string
	PluginsPath     string
	GitMirrorsPath  string
	SocketsPath     string
	JobContextDir   string
	HooksPaths      []string
	SigningJWKSFile string

	// RunInPty allocates a TTY in the container.
	RunInPty bool

	// CancelSignal is sent to bootstrap when the job is interrupted.
	CancelSignal process.Signal
}

// reservedEnv are env names the executor sets itself.
var reservedEnv = []string{"HOME", "BUILDKITE_BIN_PATH"}

// envName matches a portable environment variable name.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validate checks the configuration without touching the host or the Docker
// daemon. It returns the parsed operator mounts and the resolved operator
// env, with name-only entries looked up using lookupEnv.
func (c Config) validate(lookupEnv func(string) (string, bool)) ([]mount.Mount, []string, error) {
	if c.Image == "" {
		return nil, nil, errors.New("executor-docker-image is required when executor is docker")
	}

	// Host paths are mounted at the same path in the container, so they must
	// be absolute.
	for _, p := range []struct {
		name     string
		path     string
		required bool
	}{
		{"build-path", c.BuildPath, true},
		{"plugins-path", c.PluginsPath, true},
		{"sockets-path", c.SocketsPath, true},
		{"job-context-dir", c.JobContextDir, true},
		{"git-mirrors-path", c.GitMirrorsPath, false},
		{"signing-jwks-file", c.SigningJWKSFile, false},
	} {
		switch {
		case p.path == "" && p.required:
			return nil, nil, fmt.Errorf("%s is required when executor is docker", p.name)
		case p.path != "" && !path.IsAbs(p.path):
			return nil, nil, fmt.Errorf("%s must be an absolute path when executor is docker, got %q", p.name, p.path)
		}
	}
	for _, p := range c.HooksPaths {
		if !path.IsAbs(p) {
			return nil, nil, fmt.Errorf("hooks-path and additional-hooks-paths must be absolute when executor is docker, got %q", p)
		}
	}

	reserved := c.reservedTargets()
	mounts := make([]mount.Mount, 0, len(c.Mounts))
	for _, spec := range c.Mounts {
		m, err := parseMount(spec)
		if err != nil {
			return nil, nil, err
		}
		if err := checkMountTarget(m.Target, reserved); err != nil {
			return nil, nil, fmt.Errorf("executor-docker-mount %q: %w", spec, err)
		}
		if slices.ContainsFunc(mounts, func(o mount.Mount) bool { return o.Target == m.Target }) {
			return nil, nil, fmt.Errorf("executor-docker-mount %q: another mount already targets %s", spec, m.Target)
		}
		mounts = append(mounts, m)
	}

	env, err := resolveEnv(c.Env, lookupEnv)
	if err != nil {
		return nil, nil, err
	}
	return mounts, env, nil
}

// reservedTargets are the cleaned container paths the executor mounts
// itself. The job log tmpfile is not included because its name is random per
// job. HOME is handled separately in checkMountTarget.
func (c Config) reservedTargets() []string {
	paths := []string{
		c.BuildPath, c.PluginsPath, c.GitMirrorsPath, c.SocketsPath, c.JobContextDir,
		c.SigningJWKSFile, agentBinDir, "/etc/passwd", "/etc/group",
	}
	paths = append(paths, c.HooksPaths...)
	paths = slices.DeleteFunc(paths, func(p string) bool { return p == "" })
	for i, p := range paths {
		paths[i] = path.Clean(p)
	}
	return paths
}

// parseMount parses an executor-docker-mount value: "src:dst" or
// "src:dst:ro" (or ":rw"), with both paths absolute.
func parseMount(spec string) (mount.Mount, error) {
	parts := strings.Split(spec, ":")
	switch {
	case len(parts) < 2:
		return mount.Mount{}, fmt.Errorf("executor-docker-mount %q: want src:dst or src:dst:ro", spec)
	case len(parts) > 3:
		return mount.Mount{}, fmt.Errorf("executor-docker-mount %q: paths may not contain ':'", spec)
	}
	m := mount.Mount{Type: mount.TypeBind, Source: parts[0], Target: parts[1]}
	if len(parts) == 3 {
		switch parts[2] {
		case "ro":
			m.ReadOnly = true
		case "rw":
		default:
			return mount.Mount{}, fmt.Errorf("executor-docker-mount %q: mode must be ro or rw, got %q", spec, parts[2])
		}
	}
	if !path.IsAbs(m.Source) || !path.IsAbs(m.Target) {
		return mount.Mount{}, fmt.Errorf("executor-docker-mount %q: both paths must be absolute", spec)
	}
	m.Source, m.Target = path.Clean(m.Source), path.Clean(m.Target)
	return m, nil
}

// checkMountTarget rejects a target that overlaps a reserved path in either
// direction, because a mount over a parent would also create mount points in
// its host source. Targets inside HOME are allowed so ssh keys and git config
// can be mounted there, but HOME itself and its parents are not.
func checkMountTarget(target string, reserved []string) error {
	if within(homeDir, target) {
		return fmt.Errorf("target is or contains the executor's HOME (%s), mount files inside it instead", homeDir)
	}
	for _, r := range reserved {
		if within(target, r) || within(r, target) {
			return fmt.Errorf("target overlaps %s, which the executor mounts itself", r)
		}
	}
	return nil
}

// within reports whether the cleaned absolute path p equals dir or is inside
// it.
func within(p, dir string) bool {
	return p == dir || dir == "/" || strings.HasPrefix(p, dir+"/")
}

// resolveEnv turns executor-docker-env values into KEY=VALUE entries, in
// order. A name-only entry takes its value from lookupEnv and must be set.
func resolveEnv(entries []string, lookupEnv func(string) (string, bool)) ([]string, error) {
	env := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name, value, hasValue := strings.Cut(entry, "=")
		switch {
		case !envName.MatchString(name):
			return nil, fmt.Errorf("executor-docker-env %q: invalid name %q", entry, name)
		case slices.Contains(reservedEnv, name):
			return nil, fmt.Errorf("executor-docker-env %q: %s is set by the executor", entry, name)
		case seen[name]:
			return nil, fmt.Errorf("executor-docker-env %q: %s is set more than once", entry, name)
		}
		seen[name] = true
		if !hasValue {
			v, ok := lookupEnv(name)
			if !ok {
				return nil, fmt.Errorf("executor-docker-env %q: %s is not set in the agent's environment", entry, name)
			}
			value = v
		}
		env = append(env, name+"="+value)
	}
	return env, nil
}
