package dockerexec

import (
	"context"
	"debug/elf"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/buildkite/agent/v4/logger"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Preflight time limits. Pulling dominates, and large images can take a
// while on a cold host.
const (
	pingTimeout    = 30 * time.Second
	pullTimeout    = 10 * time.Minute
	inspectTimeout = 30 * time.Second
)

// preflightDeps are what prepare needs from the host, injected for tests.
type preflightDeps struct {
	client          dockerClient
	agentBinary     string
	lookupEnv       func(string) (string, bool)
	dockerConfigDir string
}

// prepare runs the preflight checks in order, cheapest first, so a
// configuration mistake is reported before any daemon call.
func prepare(ctx context.Context, l logger.Logger, cfg Config, deps preflightDeps) (*Executor, error) {
	mounts, env, err := cfg.validate(deps.lookupEnv)
	if err != nil {
		return nil, err
	}
	for _, m := range mounts {
		if _, err := os.Stat(m.Source); err != nil {
			return nil, fmt.Errorf("executor-docker-mount source: %w", err)
		}
	}
	if err := checkStaticBinary(deps.agentBinary); err != nil {
		return nil, err
	}
	auth, err := registryAuth(cfg.Image, deps.dockerConfigDir)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if _, err := deps.client.Ping(pingCtx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		return nil, fmt.Errorf("connecting to the Docker daemon: %w", err)
	}

	imageID, err := pullImage(ctx, l, deps.client, cfg.Image, auth)
	if err != nil {
		return nil, err
	}

	groups, err := os.Getgroups()
	if err != nil {
		return nil, fmt.Errorf("getting the agent's supplementary groups: %w", err)
	}
	return &Executor{
		logger:      l,
		client:      deps.client,
		cfg:         cfg,
		imageID:     imageID,
		agentBinary: deps.agentBinary,
		mounts:      mounts,
		env:         env,
		uid:         os.Getuid(),
		gid:         os.Getgid(),
		groups:      groups,
	}, nil
}

// checkStaticBinary returns an error if the ELF binary at path needs a
// dynamic loader. The binary is mounted into arbitrary images, which may not
// have a compatible libc.
func checkStaticBinary(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("reading the agent binary: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return fmt.Errorf("the agent binary %s is dynamically linked, but the docker executor mounts it into the job image and needs a static build (CGO_ENABLED=0)", path)
		}
	}
	return nil
}

// pullImage pulls image for the host's platform and returns its ID. If the
// pull fails but the image is already present, it uses the local copy, which
// covers air-gapped hosts and registries the agent cannot authenticate to.
func pullImage(ctx context.Context, l logger.Logger, cli dockerClient, image, auth string) (string, error) {
	l.Infof("Pulling docker executor image %s", image)
	pullCtx, cancel := context.WithTimeout(ctx, pullTimeout)
	pullErr := pull(pullCtx, cli, image, auth)
	cancel()
	if pullErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("pulling docker executor image %s: %w", image, pullErr)
		}
		l.Warnf("Couldn't pull docker executor image %s, looking for a local copy, which may be out of date: %v", image, pullErr)
	}

	inspectCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	inspect, err := cli.ImageInspect(inspectCtx, image)
	if err != nil {
		if pullErr != nil {
			return "", fmt.Errorf("pulling docker executor image %s: %w", image, pullErr)
		}
		return "", fmt.Errorf("inspecting docker executor image %s: %w", image, err)
	}
	if inspect.Os != "linux" || inspect.Architecture != runtime.GOARCH {
		return "", fmt.Errorf("docker executor image %s is for %s/%s, but the agent host is linux/%s", image, inspect.Os, inspect.Architecture, runtime.GOARCH)
	}
	l.Infof("Using docker executor image %s (%s)", image, inspect.ID)
	return inspect.ID, nil
}

// pull pulls image for the host's platform and waits for it to finish.
func pull(ctx context.Context, cli dockerClient, image, auth string) error {
	resp, err := cli.ImagePull(ctx, image, client.ImagePullOptions{
		RegistryAuth: auth,
		Platforms:    []ocispec.Platform{{OS: "linux", Architecture: runtime.GOARCH}},
	})
	if err != nil {
		return err
	}
	defer resp.Close() //nolint:errcheck // The pull result comes from Wait.
	return resp.Wait(ctx)
}
