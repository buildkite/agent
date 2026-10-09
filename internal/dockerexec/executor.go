package dockerexec

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/v4/logger"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// Executor runs jobs in containers. Prepare builds one at agent start, and
// it is shared by every job the agent runs.
type Executor struct {
	logger logger.Logger
	client dockerClient
	cfg    Config

	// imageID is the image every container is created from. Using the ID
	// rather than cfg.Image means a tag that moves while the agent runs
	// cannot swap in an image the preflight never saw.
	imageID string

	// agentBinary is the host path of the static agent binary mounted into
	// every container.
	agentBinary string

	// mounts and env are the operator's executor-docker-mount and
	// executor-docker-env settings, parsed and resolved.
	mounts []mount.Mount
	env    []string

	// The agent process's identity, which the container runs as.
	uid, gid int
	groups   []int

	// runID identifies this agent run in container labels.
	runID string
}

// dockerClient is the part of the Docker Engine API client the executor uses.
type dockerClient interface {
	Ping(ctx context.Context, options client.PingOptions) (client.PingResult, error)
	ImagePull(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	ImageInspect(ctx context.Context, ref string, options ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerAttach(ctx context.Context, containerID string, options client.ContainerAttachOptions) (client.ContainerAttachResult, error)
	ContainerWait(ctx context.Context, containerID string, options client.ContainerWaitOptions) client.ContainerWaitResult
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerKill(ctx context.Context, containerID string, options client.ContainerKillOptions) (client.ContainerKillResult, error)
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
}

// Prepare runs the start-time preflight and returns an Executor. It checks
// the configuration, the agent binary, and the Docker daemon, then pulls the
// image and resolves it to an ID. Any failure means the agent cannot run
// jobs with the docker executor.
func Prepare(ctx context.Context, l logger.Logger, cfg Config) (*Executor, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("the docker executor only supports Linux")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, err
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	e, err := prepare(ctx, l, cfg, preflightDeps{
		client:          cli,
		agentBinary:     exe,
		lookupEnv:       os.LookupEnv,
		dockerConfigDir: dockerConfigDir(),
	})
	if err != nil {
		_ = cli.Close()
		return nil, err
	}
	return e, nil
}

// Request holds the per-job values the executor needs.
type Request struct {
	// JobID names and labels the container.
	JobID string

	// Env is the job environment. It does not include the agent's own
	// process environment, which never reaches the container.
	Env []string

	// Output receives the container's stdout and stderr.
	Output io.Writer

	// JobLogTmpfile is the path of the job log tmpfile, or empty if
	// enable-job-log-tmpfile is off.
	JobLogTmpfile string
}
