// Package dockerbootstrap runs the normal bootstrap in a disposable Docker container.
package dockerbootstrap

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	DefaultImage    = "buildkite/agent-base:ubuntu-noble-hosted"
	ContextEnv      = "BUILDKITE_DOCKER_BOOTSTRAP_CONTEXT"
	containerRoot   = "/buildkite-docker"
	containerBinary = containerRoot + "/bin/buildkite-agent"
)

type Config struct {
	Image            string
	Binary           string
	Environment      []string
	CleanupMargin    time.Duration
	OperationTimeout time.Duration
	PullTimeout      time.Duration
}

type jobConfig struct {
	env   map[string]string
	args  []string
	grace time.Duration
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func prepare(cfg Config) (jobConfig, error) {
	var job jobConfig
	if cfg.Image == "" || strings.HasPrefix(cfg.Image, "-") || strings.ContainsAny(cfg.Image, " \t\r\n") {
		return job, fmt.Errorf("invalid Docker bootstrap image")
	}
	if cfg.CleanupMargin <= 0 || cfg.OperationTimeout <= 0 || cfg.PullTimeout <= 0 {
		return job, fmt.Errorf("docker bootstrap timeouts must be positive")
	}
	all := make(map[string]string)
	for _, pair := range cfg.Environment {
		name, value, ok := strings.Cut(pair, "=")
		if ok {
			all[name] = value
		}
	}
	outer, err := time.ParseDuration(all["BUILDKITE_CANCEL_SIGNAL_TIMEOUT"])
	if err != nil || outer <= cfg.CleanupMargin {
		return job, fmt.Errorf("cancel-signal-timeout must exceed Docker cleanup margin (%s)", cfg.CleanupMargin)
	}
	job.grace = outer - cfg.CleanupMargin
	contextDir := all[ContextEnv]
	if !filepath.IsAbs(contextDir) || filepath.Clean(contextDir) == "/" || filepath.Clean(contextDir) == "/tmp" {
		return job, fmt.Errorf("docker-bootstrap requires a private job context created by agent start with --job-context-dir")
	}
	if within(contextDir, containerRoot) || within(containerRoot, contextDir) {
		return job, fmt.Errorf("job context overlaps reserved Docker paths")
	}
	for _, name := range []string{"BUILDKITE_ENV_FILE", "BUILDKITE_ENV_JSON_FILE", "BUILDKITE_AGENT_JOB_TIMEOUT_FILE"} {
		if filepath.Dir(all[name]) != contextDir {
			return job, fmt.Errorf("%s must be inside the private job context", name)
		}
	}
	data, err := os.ReadFile(all["BUILDKITE_ENV_JSON_FILE"])
	if err != nil {
		return job, fmt.Errorf("read job environment: %w", err)
	}
	var names map[string]string
	if err := json.Unmarshal(data, &names); err != nil {
		return job, fmt.Errorf("decode job environment: %w", err)
	}
	job.env = make(map[string]string)
	for name, value := range all {
		_, fromJob := names[name]
		if fromJob || strings.HasPrefix(name, "BUILDKITE_") || strings.HasPrefix(name, "OTEL_EXPORTER_OTLP_") {
			// --env NAME uses the Docker client's own environment. Reject its
			// control variables rather than allowing jobs to configure the client.
			if strings.HasPrefix(name, "DOCKER_") {
				return job, fmt.Errorf("docker CLI control variables cannot be forwarded in the prototype")
			}
			if !envName.MatchString(name) {
				return job, fmt.Errorf("invalid job environment variable name")
			}
			job.env[name] = value
		}
	}
	for _, name := range []string{"BUILDKITE_AGENT_ENDPOINT", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		u, err := url.Parse(job.env[name])
		if err == nil && (strings.EqualFold(u.Hostname(), "localhost") || net.ParseIP(u.Hostname()).IsLoopback()) {
			return job, fmt.Errorf("%s uses a loopback address unreachable from the job container", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "BUILDKITE_JOB_LOG_TMPFILE", "BUILDKITE_CONFIG_PATH", ContextEnv} {
		delete(job.env, name)
	}
	job.env["BUILDKITE_BIN_PATH"] = filepath.Dir(containerBinary)
	job.env["BUILDKITE_SOCKETS_PATH"] = containerRoot + "/sockets"
	job.env["BUILDKITE_CANCEL_SIGNAL_TIMEOUT"] = job.grace.String()
	job.args = []string{"--init", "--rm", "--entrypoint", "/bin/sh", "--tmpfs", containerRoot + "/sockets:rw,nosuid,nodev,mode=1777"}
	if all["BUILDKITE_PTY"] != "false" {
		job.args = append(job.args, "--tty")
		job.env["TERM"] = all["TERM"]
		if job.env["TERM"] == "" {
			job.env["TERM"] = "xterm-256color"
		}
	}
	mounts := make(map[string]string)
	addMount := func(source, target string, readonly bool) error {
		if !filepath.IsAbs(source) || !filepath.IsAbs(target) || strings.ContainsAny(source+target, ",\r\n") || filepath.Clean(source) == "/" {
			return fmt.Errorf("docker bind mounts require absolute, non-root paths without commas or newlines")
		}
		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("docker mount source unavailable: %w", err)
		}
		arg := "type=bind,src=" + source + ",dst=" + target
		if readonly {
			arg += ",readonly"
		}
		if prior, ok := mounts[target]; ok && prior != arg {
			return fmt.Errorf("conflicting Docker mounts at %s", target)
		}
		mounts[target] = arg
		return nil
	}
	if err := addMount(cfg.Binary, containerBinary, true); err != nil {
		return job, err
	}
	if err := addMount(contextDir, contextDir, true); err != nil {
		return job, err
	}
	for _, name := range []string{"BUILDKITE_BUILD_PATH", "BUILDKITE_PLUGINS_PATH", "BUILDKITE_HOOKS_PATH", "BUILDKITE_GIT_MIRRORS_PATH", "BUILDKITE_ADDITIONAL_HOOKS_PATHS"} {
		for _, path := range strings.Split(all[name], ",") {
			if path == "" {
				continue
			}
			path = filepath.Clean(path)
			if path == "/tmp" || within(contextDir, path) || within(containerRoot, path) || within(path, containerRoot) {
				return job, fmt.Errorf("%s overlaps private Docker paths", name)
			}
			readonly := strings.Contains(name, "HOOKS_PATH")
			if readonly {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					continue
				}
			}
			if !readonly {
				if !filepath.IsAbs(path) {
					return job, fmt.Errorf("%s must be absolute", name)
				}
				if err := os.MkdirAll(path, 0o755); err != nil {
					return job, err
				}
			}
			if err := addMount(path, path, readonly); err != nil {
				return job, err
			}
		}
	}
	if all["BUILDKITE_BUILD_PATH"] == "" {
		return job, fmt.Errorf("BUILDKITE_BUILD_PATH is required")
	}
	job.args = append(job.args, "--workdir", all["BUILDKITE_BUILD_PATH"])
	// Expose only the agent socket, never the shared sockets directory.
	if base := all["BUILDKITE_SOCKETS_PATH"]; base != "" {
		for _, name := range []string{"agent-" + all["BUILDKITE_AGENT_PID"], "agent-leader"} {
			source := filepath.Join(base, name)
			if info, err := os.Stat(source); err == nil && info.Mode()&os.ModeSocket != 0 {
				if err := addMount(source, containerRoot+"/sockets/"+name, false); err != nil {
					return job, err
				}
			}
		}
	}
	keys := make([]string, 0, len(mounts))
	for target := range mounts {
		keys = append(keys, target)
	}
	slices.Sort(keys)
	for _, target := range keys {
		job.args = append(job.args, "--mount", mounts[target])
	}
	keys = keys[:0]
	for name := range job.env {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	for _, name := range keys {
		job.args = append(job.args, "--env", name)
	}
	return job, nil
}

func within(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+string(filepath.Separator))
}
