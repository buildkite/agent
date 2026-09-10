package clicommand

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/internal/dockerbootstrap"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/internal/self"
	"github.com/urfave/cli/v3"
)

// DockerBootstrapConfig retains cli tags for config completeness checks only.
type DockerBootstrapConfig struct {
	PullPolicy       string        `cli:"pull-policy"`
	DockerConfig     string        `cli:"docker-config"`
	HelperEnvFile    string        `cli:"docker-helper-env-file"`
	HelperPath       string        `cli:"docker-helper-path"`
	User             string        `cli:"user"`
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
Registry credentials are read only from an operator-supplied --docker-config.`,
	Flags: []cli.Flag{
		// Job environment must not select operator policy.
		&cli.StringFlag{Name: "pull-policy", Value: "missing", Usage: "Image pull policy: always, missing, or never"},
		&cli.StringFlag{Name: "docker-config", Usage: "Absolute host Docker configuration directory for registry authentication"},
		&cli.StringFlag{Name: "docker-helper-env-file", Usage: "Private host JSON file of credential helper environment variables"},
		&cli.StringFlag{Name: "docker-helper-path", Usage: "Absolute host PATH entries for registry credential helpers"},
		&cli.StringFlag{Name: "user", Usage: "Container numeric UID:GID (default: host agent UID:GID)"},
		&cli.StringFlag{Name: "image", Value: dockerbootstrap.DefaultImage, Usage: "Agent-controlled bootstrap image"},
		&cli.StringFlag{Name: "docker-path", Value: "/usr/bin/docker", Usage: "Absolute path to the Docker CLI"},
		&cli.DurationFlag{Name: "cleanup-margin", Value: 5 * time.Second, Usage: "Time reserved within the agent cancellation timeout for removal"},
		&cli.DurationFlag{Name: "operation-timeout", Value: 30 * time.Second, Usage: "Timeout for Docker setup operations"},
		&cli.DurationFlag{Name: "pull-timeout", Value: 10 * time.Minute, Usage: "Timeout for pulling the bootstrap image"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		// The general config loader would also trust inherited config-file settings.
		cfg := DockerBootstrapConfig{
			Image: c.String("image"), DockerPath: c.String("docker-path"),
			PullPolicy: c.String("pull-policy"), DockerConfig: c.String("docker-config"), HelperEnvFile: c.String("docker-helper-env-file"), HelperPath: c.String("docker-helper-path"), User: c.String("user"),
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
		// An empty config prevents use of host registry credentials.
		configDir, err := os.MkdirTemp("", "buildkite-docker-config-")
		if err != nil {
			return cli.Exit(err, dockerbootstrap.SetupFailure)
		}
		defer func() { _ = os.RemoveAll(configDir) }()
		auth, err := dockerbootstrap.LoadAuthentication(cfg.DockerConfig, cfg.HelperEnvFile, cfg.HelperPath)
		if err != nil {
			return cli.Exit(err, dockerbootstrap.SetupFailure)
		}
		authDir := ""
		if auth.Config != nil {
			authDir = filepath.Join(configDir, "auth")
			if err := os.Mkdir(authDir, 0o700); err != nil {
				return cli.Exit(err, dockerbootstrap.SetupFailure)
			}
			if err := os.WriteFile(filepath.Join(authDir, "config.json"), auth.Config, 0o600); err != nil {
				return cli.Exit(err, dockerbootstrap.SetupFailure)
			}
		}
		runner := dockerbootstrap.Runner{
			Client: dockerbootstrap.CLI{Path: cfg.DockerPath, ConfigDir: configDir, AuthConfigDir: authDir, HelperPath: cfg.HelperPath, HelperEnvironment: auth.Environment},
			Stdout: os.Stdout, Stderr: os.Stderr,
		}
		code, err := runner.Run(ctx, dockerbootstrap.Config{
			PullPolicy: cfg.PullPolicy, DockerConfig: cfg.DockerConfig, HelperEnvFile: cfg.HelperEnvFile, User: cfg.User, RedactedValues: auth.Secrets,
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
