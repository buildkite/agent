package dockerexec

import (
	"context"
	"crypto/rand"
	"debug/elf"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/buildkite/agent/v4/logger"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
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
	if cfg.Image != "" && digest.Digest(cfg.Image).Validate() != nil {
		if _, err := reference.ParseNormalizedNamed(cfg.Image); err != nil {
			return nil, fmt.Errorf("invalid executor-docker-image: %w", err)
		}
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if _, err := deps.client.Ping(pingCtx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		return nil, fmt.Errorf("connecting to the Docker daemon: %w", err)
	}

	groups, err := os.Getgroups()
	if err != nil {
		return nil, fmt.Errorf("getting the agent's supplementary groups: %w", err)
	}
	return &Executor{
		logger:          l,
		client:          deps.client,
		cfg:             cfg,
		dockerConfigDir: deps.dockerConfigDir,
		agentBinary:     deps.agentBinary,
		mounts:          mounts,
		env:             env,
		uid:             os.Getuid(),
		gid:             os.Getgid(),
		groups:          groups,
		runID:           rand.Text(),
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

// resolveImage refreshes tags, but uses an already cached digest without a pull.
func (e *Executor) resolveImage(ctx context.Context, image string) (string, error) {
	if image == "" {
		return "", fmt.Errorf("job has no resolved image and --executor-docker-image is not set")
	}
	if image == e.cfg.Image && digest.Digest(image).Validate() == nil {
		return e.inspectImage(ctx, image)
	}
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", fmt.Errorf("invalid job image %q: %w", image, err)
	}
	if _, pinned := named.(reference.Digested); pinned {
		id, err := e.inspectImage(ctx, image)
		if !cerrdefs.IsNotFound(err) {
			return id, err
		}
	}
	auth, err := registryAuth(image, e.dockerConfigDir)
	if err != nil {
		return "", err
	}
	pullCtx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	if err := pull(pullCtx, e.client, image, auth); err != nil {
		return "", fmt.Errorf("pulling job image %s: %w", image, err)
	}
	return e.inspectImage(ctx, image)
}

func (e *Executor) inspectImage(ctx context.Context, image string) (string, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	inspect, err := e.client.ImageInspect(inspectCtx, image)
	if err != nil {
		return "", fmt.Errorf("inspecting docker executor image %s: %w", image, err)
	}
	if inspect.Os != "linux" || inspect.Architecture != runtime.GOARCH {
		return "", fmt.Errorf("docker executor image %s is for %s/%s, but the agent host is linux/%s", image, inspect.Os, inspect.Architecture, runtime.GOARCH)
	}
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
