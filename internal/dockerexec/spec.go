package dockerexec

import (
	"fmt"
	"maps"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Container labels, so an operator can find containers a crashed agent left
// behind and tell them apart from a running agent's.
const (
	labelJobID    = "com.buildkite.job-id"
	labelAgentRun = "com.buildkite.agent-run"
)

// createOptions returns the request that creates the container named name
// for req. The image supplies only its filesystem and ENV: the executor sets
// the entrypoint, command, user, working directory, and init. exists reports
// whether an optional read-only mount source is present.
func (e *Executor) createOptions(req Request, name string, exists func(string) bool) client.ContainerCreateOptions {
	groups := make([]string, 0, len(e.groups))
	for _, g := range e.groups {
		groups = append(groups, strconv.Itoa(g))
	}
	useInit := true
	return client.ContainerCreateOptions{
		Name:     name,
		Platform: &ocispec.Platform{OS: "linux", Architecture: runtime.GOARCH},
		Config: &container.Config{
			Image:      e.imageID,
			Entrypoint: []string{agentBinPath},
			Cmd:        []string{"bootstrap"},
			Env:        containerEnv(e.env, req.Env, req.JobLogTmpfile),
			User:       fmt.Sprintf("%d:%d", e.uid, e.gid),
			WorkingDir: e.cfg.BuildPath,
			Tty:        e.cfg.RunInPty,
			Labels: map[string]string{
				labelJobID:    req.JobID,
				labelAgentRun: e.runID,
			},
		},
		HostConfig: &container.HostConfig{
			Mounts: e.containerMounts(req, exists),
			// Docker mounts tmpfs noexec by default, which would break tools
			// that install binaries under HOME.
			Tmpfs:       map[string]string{homeDir: fmt.Sprintf("exec,uid=%d,gid=%d,mode=0700", e.uid, e.gid)},
			GroupAdd:    groups,
			SecurityOpt: []string{"no-new-privileges"},
			Init:        &useInit,
			NetworkMode: container.NetworkMode(e.cfg.Network),
		},
	}
}

// containerMounts returns the bind mounts for req. Host paths are mounted at
// the same path in the container so nothing in bootstrap, hooks, or plugins
// needs to learn a new layout. Optional read-only sources that do not exist
// are left out, so bootstrap reports the problem rather than the daemon. If
// two settings name the same path, the first mount wins, because the daemon
// rejects duplicate targets.
func (e *Executor) containerMounts(req Request, exists func(string) bool) []mount.Mount {
	bind := func(p string, readOnly bool) mount.Mount {
		p = path.Clean(p)
		return mount.Mount{Type: mount.TypeBind, Source: p, Target: p, ReadOnly: readOnly}
	}

	mounts := []mount.Mount{
		bind(e.cfg.BuildPath, false),
		bind(e.cfg.PluginsPath, false),
		bind(e.cfg.SocketsPath, false),
		bind(e.cfg.JobContextDir, false),
	}
	if e.cfg.GitMirrorsPath != "" {
		mounts = append(mounts, bind(e.cfg.GitMirrorsPath, false))
	}
	for _, p := range append(slices.Clone(e.cfg.HooksPaths), e.cfg.SigningJWKSFile) {
		if p != "" && exists(p) {
			mounts = append(mounts, bind(p, true))
		}
	}
	if req.JobLogTmpfile != "" {
		mounts = append(mounts, bind(req.JobLogTmpfile, true))
	}
	mounts = append(mounts,
		bind("/etc/passwd", true),
		bind("/etc/group", true),
		mount.Mount{Type: mount.TypeBind, Source: e.agentBinary, Target: agentBinPath, ReadOnly: true},
	)
	mounts = append(mounts, e.mounts...)

	seen := make(map[string]bool, len(mounts))
	return slices.DeleteFunc(mounts, func(m mount.Mount) bool {
		dup := seen[m.Target]
		seen[m.Target] = true
		return dup
	})
}

// containerEnv builds the container env in layers, later layers winning:
// operator env, then the job env, then the values the executor owns. The job
// env overrides the operator's, as it overrides the agent's own environment
// with the exec executor. Each key appears once, sorted. The image's ENV sits
// underneath all of these.
func containerEnv(operatorEnv, jobEnv []string, jobLogTmpfile string) []string {
	env := make(map[string]string)
	for _, kv := range slices.Concat(operatorEnv, jobEnv) {
		// Both lists are built by the agent as KEY=VALUE entries, so anything
		// else is not an env entry at all.
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			env[k] = v
		}
	}

	// Nothing in the job uses the agent's PID, and the agent config file is
	// not mounted.
	delete(env, "BUILDKITE_AGENT_PID")
	delete(env, "BUILDKITE_CONFIG_PATH")

	env["HOME"] = homeDir
	env["BUILDKITE_BIN_PATH"] = agentBinDir
	// The executor is told the tmpfile path because it mounts the file, so it
	// sets the variable itself rather than relying on the job env.
	delete(env, "BUILDKITE_JOB_LOG_TMPFILE")
	if jobLogTmpfile != "" {
		env["BUILDKITE_JOB_LOG_TMPFILE"] = jobLogTmpfile
	}

	out := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		out = append(out, k+"="+env[k])
	}
	return out
}
