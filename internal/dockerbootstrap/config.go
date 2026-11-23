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
	"unicode/utf8"
)

const (
	DefaultImage    = "buildkite/agent-base:ubuntu-noble-hosted"
	ContextEnv      = "BUILDKITE_DOCKER_BOOTSTRAP_CONTEXT"
	containerRoot   = "/buildkite-docker"
	containerBinary = "/bin/buildkite-agent"
)

type Config struct {
	PullPolicy        string
	DockerConfig      string
	HelperEnvFile     string
	User              string
	RedactedValues    []string
	Image             string
	ImageFromJob      bool
	MountDockerSocket bool
	DockerPath        string
	DockerCLIPlugins  string
	Binary            string
	Environment       []string
	CleanupMargin     time.Duration
	OperationTimeout  time.Duration
	PullTimeout       time.Duration
}

type jobConfig struct {
	image         string
	env           map[string]string
	args          []string
	grace         time.Duration
	userDirectory string
	uid, gid      int
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var proxyNames = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"}

func prepare(cfg Config) (jobConfig, error) {
	var job jobConfig
	if _, _, err := containerUser(cfg.User); err != nil {
		return job, err
	}
	switch cfg.PullPolicy {
	case "", "missing", "always", "never":
	default:
		return job, fmt.Errorf("pull-policy must be always, missing, or never")
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
	job.image = cfg.Image
	if cfg.ImageFromJob {
		// Read the API job snapshot, not the inherited process environment.
		// The server must resolve BUILDKITE_IMAGE after merging user env.
		job.image = names["BUILDKITE_IMAGE"]
	}
	if job.image == "" || strings.HasPrefix(job.image, "-") || strings.ContainsAny(job.image, " \t\r\n") {
		return job, fmt.Errorf("invalid or missing Docker bootstrap image (image-from-job requires server-resolved BUILDKITE_IMAGE)")
	}
	job.env = make(map[string]string)
	for name, value := range all {
		_, fromJob := names[name]
		// Host proxy settings are forwarded so the container reaches the network
		// the same way the agent does; job values take precedence.
		if fromJob || strings.HasPrefix(name, "BUILDKITE_") || strings.HasPrefix(name, "OTEL_EXPORTER_OTLP_") || slices.Contains(proxyNames, name) {
			if !envName.MatchString(name) {
				return job, fmt.Errorf("invalid job environment variable name")
			}
			job.env[name] = value
		}
	}
	if cfg.ImageFromJob {
		job.env["BUILDKITE_IMAGE"] = job.image
	}
	for _, name := range []string{"BUILDKITE_AGENT_ENDPOINT", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		value := job.env[name]
		// Proxy clients also accept host:port without a URL scheme.
		if value != "" && !strings.Contains(value, "://") && !strings.HasPrefix(value, "//") {
			value = "//" + value
		}
		u, err := url.Parse(value)
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
		if err := rejectCredentialMount(source, cfg); err != nil {
			return err
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
	if cfg.DockerCLIPlugins != "" && !cfg.MountDockerSocket {
		return job, fmt.Errorf("docker-cli-plugins requires --mount-docker-socket")
	}
	if cfg.MountDockerSocket {
		info, err := os.Stat("/var/run/docker.sock")
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			return job, fmt.Errorf("mount-docker-socket requires a local Docker socket at /var/run/docker.sock")
		}
		for _, mount := range [][2]string{
			{"/var/run/docker.sock", "/var/run/docker.sock"},
			{cfg.DockerPath, "/usr/bin/docker"},
			{cfg.DockerCLIPlugins, "/usr/local/lib/docker/cli-plugins"},
		} {
			if mount[0] == "" && mount[1] == "/usr/local/lib/docker/cli-plugins" {
				continue
			}
			if err := addMount(mount[0], mount[1], true); err != nil {
				return job, err
			}
		}
		// Job DOCKER_HOST/CONTEXT must not redirect this explicit local access.
		job.env["DOCKER_HOST"] = "unix:///var/run/docker.sock"
		for _, name := range []string{"DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
			job.env[name] = ""
		}
	}
	if err := addMount(contextDir, contextDir, true); err != nil {
		return job, err
	}
	for _, name := range []string{"BUILDKITE_BUILD_PATH", "BUILDKITE_PLUGINS_PATH", "BUILDKITE_HOOKS_PATH", "BUILDKITE_GIT_MIRRORS_PATH", "BUILDKITE_ADDITIONAL_HOOKS_PATHS"} {
		paths := []string{all[name]}
		if name == "BUILDKITE_ADDITIONAL_HOOKS_PATHS" {
			paths = strings.Split(all[name], ",")
		}
		for _, path := range paths {
			if path == "" {
				continue
			}
			// Splitting scalar paths on commas could expose unintended host directories.
			if !filepath.IsAbs(path) || strings.ContainsAny(path, ",\r\n") {
				return job, fmt.Errorf("%s requires absolute paths without commas or newlines", name)
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
	job.args = append(job.args, "--workdir", containerRoot+"/home")
	// Mounting the shared directory would expose other jobs' sockets.
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
	userDir, uid, gid, err := prepareUser(cfg.User, contextDir)
	if err != nil {
		return job, err
	}
	job.userDirectory = userDir
	job.uid, job.gid = uid, gid
	job.args = append(job.args, "--user", fmt.Sprintf("%d:%d", uid, gid), "--tmpfs", fmt.Sprintf("%s/home:rw,nosuid,nodev,mode=0700,uid=%d,gid=%d", containerRoot, uid, gid))
	job.env["HOME"] = containerRoot + "/home"
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
	// --env NAME makes the host Docker client read the value from its own
	// environment, where DOCKER_* variables would also configure the client.
	// Pass those through a private env file instead.
	var dockerEnv strings.Builder
	for _, name := range keys {
		if !strings.HasPrefix(name, "DOCKER_") {
			job.args = append(job.args, "--env", name)
			continue
		}
		value := job.env[name]
		delete(job.env, name)
		if strings.ContainsAny(value, "\r\n") || !utf8.ValidString(value) {
			_ = os.RemoveAll(userDir)
			return job, fmt.Errorf("%s must be a single line of UTF-8 text", name)
		}
		dockerEnv.WriteString(name + "=" + value + "\n")
	}
	if dockerEnv.Len() > 0 {
		path := filepath.Join(userDir, "docker.env")
		if err := os.WriteFile(path, []byte(dockerEnv.String()), 0o600); err != nil {
			_ = os.RemoveAll(userDir)
			return job, err
		}
		job.args = append(job.args, "--env-file", path)
	}
	return job, nil
}

func within(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+string(filepath.Separator))
}
