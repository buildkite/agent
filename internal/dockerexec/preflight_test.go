package dockerexec

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

// newPreflightFixture returns a valid config, a static agent binary, and a
// fake daemon that pulls and inspects a matching image successfully.
func newPreflightFixture(t *testing.T) (Config, preflightDeps, *fakeClient) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the docker executor only supports Linux")
	}
	cfg := validConfig()
	cfg.Mounts = []string{t.TempDir() + ":/cache"}
	cfg.Env = []string{"HTTP_PROXY"}
	fake := &fakeClient{
		t:         t,
		ping:      func() error { return nil },
		imagePull: func(string, client.ImagePullOptions) error { return nil },
		imageInspect: func(string) (client.ImageInspectResult, error) {
			return client.ImageInspectResult{InspectResponse: image.InspectResponse{
				ID: "sha256:abc", Os: "linux", Architecture: runtime.GOARCH,
			}}, nil
		},
	}
	deps := preflightDeps{
		client:      fake,
		agentBinary: staticBinary(t),
		lookupEnv: func(name string) (string, bool) {
			return "http://proxy:3128", name == "HTTP_PROXY"
		},
		dockerConfigDir: t.TempDir(),
	}
	writeDockerConfig(t, deps.dockerConfigDir, `{"auths": {"https://index.docker.io/v1/": {"username": "alice", "password": "pw"}}}`)
	return cfg, deps, fake
}

func TestPrepare_PinsThePulledImage(t *testing.T) {
	t.Parallel()
	cfg, deps, fake := newPreflightFixture(t)
	var pullOpts client.ImagePullOptions
	fake.imagePull = func(_ string, opts client.ImagePullOptions) error {
		pullOpts = opts
		return nil
	}

	e, err := prepare(t.Context(), logger.Discard, cfg, deps)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if diff := cmp.Diff([]string{"Ping", "ImagePull", "ImageInspect"}, fake.Calls()); diff != "" {
		t.Errorf("daemon calls diff (-want +got):\n%s", diff)
	}
	if got, want := len(pullOpts.Platforms), 1; got != want {
		t.Fatalf("len(pull platforms) = %d, want %d", got, want)
	}
	if got := pullOpts.Platforms[0]; got.OS != "linux" || got.Architecture != runtime.GOARCH {
		t.Errorf("pull platform = %s/%s, want linux/%s", got.OS, got.Architecture, runtime.GOARCH)
	}
	wantAuth := &registry.AuthConfig{Username: "alice", Password: "pw", ServerAddress: "https://index.docker.io/v1/"}
	if diff := cmp.Diff(wantAuth, decodeAuth(t, pullOpts.RegistryAuth)); diff != "" {
		t.Errorf("pull registry auth diff (-want +got):\n%s", diff)
	}
	if got, want := e.imageID, "sha256:abc"; got != want {
		t.Errorf("imageID = %q, want %q", got, want)
	}
	if diff := cmp.Diff([]string{"HTTP_PROXY=http://proxy:3128"}, e.env); diff != "" {
		t.Errorf("env diff (-want +got):\n%s", diff)
	}
	if got, want := len(e.mounts), 1; got != want {
		t.Errorf("len(mounts) = %d, want %d", got, want)
	}
}

func TestPrepare_UsesALocalImageWhenThePullFails(t *testing.T) {
	t.Parallel()
	cfg, deps, fake := newPreflightFixture(t)
	fake.imagePull = func(string, client.ImagePullOptions) error { return errors.New("registry unreachable") }

	e, err := prepare(t.Context(), logger.Discard, cfg, deps)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if got, want := e.imageID, "sha256:abc"; got != want {
		t.Errorf("imageID = %q, want %q", got, want)
	}
}

func TestPrepare_DoesNotFallBackToALocalImageWhenCancelled(t *testing.T) {
	t.Parallel()
	cfg, deps, fake := newPreflightFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	fake.imagePull = func(string, client.ImagePullOptions) error {
		cancel()
		return context.Canceled
	}

	if _, err := prepare(ctx, logger.Discard, cfg, deps); err == nil {
		t.Fatal("prepare() error = nil, want an error")
	}
	if got, want := strings.Join(fake.Calls(), ","), "Ping,ImagePull"; got != want {
		t.Errorf("daemon calls = [%s], want [%s]", got, want)
	}
}

func TestPrepare_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(*Config, *preflightDeps, *fakeClient)
		wantErr   string
		wantCalls []string
	}{
		{
			name:      "invalid_config",
			setup:     func(c *Config, _ *preflightDeps, _ *fakeClient) { c.Image = "" },
			wantErr:   "executor-docker-image is required",
			wantCalls: []string{},
		},
		{
			name: "missing_mount_source",
			setup: func(c *Config, _ *preflightDeps, _ *fakeClient) {
				c.Mounts = []string{"/does/not/exist:/cache"}
			},
			wantErr:   "executor-docker-mount source",
			wantCalls: []string{},
		},
		{
			name: "dynamic_agent_binary",
			setup: func(_ *Config, d *preflightDeps, _ *fakeClient) {
				d.agentBinary = dynamicBinary(t)
			},
			wantErr:   "dynamically linked",
			wantCalls: []string{},
		},
		{
			name: "daemon_unreachable",
			setup: func(_ *Config, _ *preflightDeps, f *fakeClient) {
				f.ping = func() error { return errors.New("connection refused") }
			},
			wantErr:   "connecting to the Docker daemon",
			wantCalls: []string{"Ping"},
		},
		{
			name: "pull_fails_and_no_local_image",
			setup: func(_ *Config, _ *preflightDeps, f *fakeClient) {
				f.imagePull = func(string, client.ImagePullOptions) error { return errors.New("manifest unknown") }
				f.imageInspect = func(string) (client.ImageInspectResult, error) {
					return client.ImageInspectResult{}, errors.New("no such image")
				}
			},
			wantErr:   "manifest unknown",
			wantCalls: []string{"Ping", "ImagePull", "ImageInspect"},
		},
		{
			name: "wrong_architecture",
			setup: func(_ *Config, _ *preflightDeps, f *fakeClient) {
				f.imageInspect = func(string) (client.ImageInspectResult, error) {
					return client.ImageInspectResult{InspectResponse: image.InspectResponse{
						ID: "sha256:abc", Os: "linux", Architecture: "s390x",
					}}, nil
				}
			},
			wantErr:   "is for linux/s390x",
			wantCalls: []string{"Ping", "ImagePull", "ImageInspect"},
		},
		{
			name: "malformed_docker_config",
			setup: func(_ *Config, d *preflightDeps, _ *fakeClient) {
				writeDockerConfig(t, d.dockerConfigDir, "{not json")
			},
			wantErr:   "parsing Docker config",
			wantCalls: []string{},
		},
		{
			name:      "invalid_image_reference",
			setup:     func(c *Config, _ *preflightDeps, _ *fakeClient) { c.Image = "Not A Valid:Ref" },
			wantErr:   "invalid reference format",
			wantCalls: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, deps, fake := newPreflightFixture(t)
			tc.setup(&cfg, &deps, fake)

			_, err := prepare(t.Context(), logger.Discard, cfg, deps)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("prepare() error = %v, want an error containing %q", err, tc.wantErr)
			}
			if got, want := strings.Join(fake.Calls(), ","), strings.Join(tc.wantCalls, ","); got != want {
				t.Errorf("daemon calls = [%s], want [%s]", got, want)
			}
		})
	}
}

func TestCheckStaticBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("needs a dynamically linked ELF binary from the host")
	}

	if err := checkStaticBinary(staticBinary(t)); err != nil {
		t.Errorf("checkStaticBinary(static) error = %v", err)
	}
	if err := checkStaticBinary(dynamicBinary(t)); err == nil {
		t.Error("checkStaticBinary(dynamic) error = nil, want an error")
	}
	if err := checkStaticBinary(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("checkStaticBinary(missing) error = nil, want an error")
	}
}
