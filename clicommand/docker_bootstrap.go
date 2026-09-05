package clicommand

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/internal/dockerbootstrap"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/internal/self"
	"github.com/urfave/cli/v3"
)

type DockerBootstrapConfig struct {
	Image            string        `cli:"image"`
	DockerPath       string        `cli:"docker-path"`
	CleanupMargin    time.Duration `cli:"cleanup-margin"`
	OperationTimeout time.Duration `cli:"operation-timeout"`
	PullTimeout      time.Duration `cli:"pull-timeout"`
}

var DockerBootstrapCommand = &cli.Command{
	Name:     "docker-bootstrap",
	Usage:    "Run the job bootstrap in a disposable local Docker container (experimental)",
	Category: categoryInternal,
	Description: `Usage:

    buildkite-agent docker-bootstrap [options...]

Experimental Linux-only supervisor for the normal job bootstrap. Configure agent
start with --bootstrap-script="/usr/bin/buildkite-agent docker-bootstrap" and a
dedicated --job-context-dir. Policy flags are accepted only as command arguments.
The default image is public; private registry authentication is not yet supported.`,
	Flags: []cli.Flag{
		// No environment sources: these are operator policy, not job inputs.
		&cli.StringFlag{Name: "image", Value: dockerbootstrap.DefaultImage, Usage: "Agent-controlled bootstrap image"},
		&cli.StringFlag{Name: "docker-path", Value: "/usr/bin/docker", Usage: "Absolute path to the Docker CLI"},
		&cli.DurationFlag{Name: "cleanup-margin", Value: 5 * time.Second, Usage: "Time reserved within the agent cancellation timeout for removal"},
		&cli.DurationFlag{Name: "operation-timeout", Value: 30 * time.Second, Usage: "Timeout for Docker setup operations"},
		&cli.DurationFlag{Name: "pull-timeout", Value: 10 * time.Minute, Usage: "Timeout for pulling the bootstrap image"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		// Read flags directly: the general config loader also reads inherited
		// config-file settings, which are not trusted Docker policy inputs.
		cfg := DockerBootstrapConfig{
			Image: c.String("image"), DockerPath: c.String("docker-path"),
			CleanupMargin: c.Duration("cleanup-margin"), OperationTimeout: c.Duration("operation-timeout"), PullTimeout: c.Duration("pull-timeout"),
		}
		if runtime.GOOS != "linux" {
			return cli.Exit("docker-bootstrap requires a Linux host", dockerbootstrap.SetupFailure)
		}
		sig := os.Getenv("BUILDKITE_CANCEL_SIGNAL")
		if sig == "" || sig == "SIGKILL" {
			sig = "SIGTERM"
		}
		parsed, err := process.ParseSignal(sig)
		if err != nil {
			return cli.Exit(err, dockerbootstrap.SetupFailure)
		}
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.Signal(parsed))
		defer stop()
		// The public default image needs no registry credentials. Private
		// registry authentication is deliberately deferred to the MVP.
		configDir, err := os.MkdirTemp("", "buildkite-docker-config-")
		if err != nil {
			return cli.Exit(err, dockerbootstrap.SetupFailure)
		}
		defer func() { _ = os.RemoveAll(configDir) }()
		runner := dockerbootstrap.Runner{
			Client: dockerbootstrap.CLI{Path: cfg.DockerPath, ConfigDir: configDir},
			Stdout: os.Stdout, Stderr: os.Stderr,
		}
		code, err := runner.Run(ctx, dockerbootstrap.Config{
			Image: cfg.Image, Binary: self.Path(ctx), Environment: os.Environ(),
			CleanupMargin: cfg.CleanupMargin, OperationTimeout: cfg.OperationTimeout, PullTimeout: cfg.PullTimeout,
		})
		if err != nil {
			return cli.Exit(fmt.Sprintf("Docker bootstrap: %v", err), dockerbootstrap.SetupFailure)
		}
		if code != 0 {
			return cli.Exit("", code)
		}
		return nil
	},
}
