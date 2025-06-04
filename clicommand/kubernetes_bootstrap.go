package clicommand

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const kubernetesBootstrapHelpDescription = `Usage:

     buildkite-agent kubernetes-bootstrap [options...]

Description:

This command is used internally by Buildkite Kubernetes jobs. It is not
intended to be used directly.`

type KubernetesBootstrapConfig struct {
	KubernetesContainerID int `cli:"kubernetes-container-id"`

	// Global flags for debugging, etc
	LogLevel    string   `cli:"log-level"`
	Debug       bool     `cli:"debug"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var KubernetesBootstrapCommand = cli.Command{
	Name:        "kubernetes-bootstrap",
	Usage:       "Rebootstraps the command after connecting to the Kubernetes socket",
	Description: bootstrapHelpDescription,
	Flags: []cli.Flag{
		KubernetesContainerIDFlag,

		// Global flags for debugging, etc
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[KubernetesBootstrapConfig](ctx, c)
		defer done()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Connect the socket.
		socket := &kubernetes.Client{ID: cfg.KubernetesContainerID}

		// Registration passes down the env vars the agent normally sets on the
		// subprocess, but in this case the bootstrap is in a separate
		// container.
		rtr := roko.NewRetrier(
			roko.WithMaxAttempts(7),
			roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
		)
		regResp, err := roko.DoFunc(ctx, rtr, func(rtr *roko.Retrier) (*kubernetes.RegisterResponse, error) {
			return socket.Connect(ctx)
		})
		if err != nil {
			return fmt.Errorf("error connecting to kubernetes runner: %w", err)
		}

		// Start with the registration response env, then override with our
		// existing env.
		// This is important because we're given higher-priority info from
		// agent-stack-k8s or the container's default setup. Examples:
		// - agent-stack-k8s interprets the job definition itself, and sets
		//   BUILDKITE_COMMAND to one that could be radically different to the
		//   one the agent normally sets.
		// - Similarly, bootstrap phases varies depending on whether this is a
		//   checkout or command container. The agent would have us run all
		//   phases.
		// - Container ID should be preserved in case of Hyrum's Law.
		// - Sockets path is set by agent-stack-k8s as it varies by container
		//   name.
		// - We don't want to use the agent container's HOME, KUBERNETES_*, etc.
		environ := env.FromSlice(slices.Concat(regResp.Env, os.Environ()))

		// Capture parameters from the agent that affect how the subprocess
		// should be run: build path, PTY, cancel signal, and signal grace period.
		buildPath := environ.GetString("BUILDKITE_BUILD_PATH", "/workspace/build")
		runInPTY := environ.GetBool("BUILDKITE_PTY", true)
		cancelSignal := process.SIGTERM
		if sig, has := environ.Get("BUILDKITE_CANCEL_SIGNAL"); has {
			cs, err := process.ParseSignal(sig)
			if err != nil {
				return err
			}
			cancelSignal = cs
		}
		cgp := environ.GetInt("BUILDKITE_CANCEL_GRACE_PERIOD", defaultCancelGracePeriodSecs)
		sgp := environ.GetInt("BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS", defaultSignalGracePeriodSecs)
		signalGracePeriod, err := signalGracePeriod(cgp, sgp)
		if err != nil {
			return err
		}

		// Ensure the Kubernetes socket setup is disabled in the subprocess
		// (we're doing all that here).
		environ.Set("BUILDKITE_KUBERNETES_EXEC", "false")

		// BUILDKITE_BIN_PATH is a funny one. The bootstrap adds it to PATH,
		// and the agent deduces it from its own path (as we do below), but in
		// the k8s stack the agent could run from two different locations:
		// - /usr/local/bin (agent, checkout container)
		// - /workspace (command containers with arbitrary images)
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding absolute path to executable: %w", err)
		}
		environ.Set("BUILDKITE_BIN_PATH", filepath.Dir(self))

		// So that the agent doesn't exit early thinking the client is lost, we want
		// to continue talking to the agent container for as long as possible (after
		// Interrupt). Hence detach the StatusLoop context from cancellation using
		// [context.WithoutCancel]. The goroutine will exit with the process.
		// (Why even have a context arg? Testing and possible future value-passing)
		if err := socket.StatusLoop(context.WithoutCancel(ctx), func(err error) {
			// If the k8s client is interrupted for any reason (either the server
			// is in state interrupted or the connection died or ...), we should
			// cancel the job.
			if err != nil {
				l.Error("Error waiting for client interrupt: %v", err)
			}
			cancel()
		}); err != nil {
			return fmt.Errorf("connecting to k8s socket: %w", err)
		}

		phases := environ.GetString("BUILDKITE_BOOTSTRAP_PHASES", "(unknown)")
		fmt.Fprintf(socket, "~~~ Bootstrapping phases %s\n", phases)

		// Now we can run the real `buildkite-agent bootstrap`.
		// Compare with the setup in [agent.NewJobRunner].
		// Tee both stdout and stderr to the k8s socket client, so that the
		// logs are shipped to the agent container and then to Buildkite, but
		// are also visible as container logs.
		proc := process.New(l, process.Config{
			Path:              self, // TODO: support custom bootstrap scripts?
			Args:              []string{"bootstrap"},
			Env:               environ.ToSlice(),
			Stdout:            io.MultiWriter(os.Stdout, socket),
			Stderr:            io.MultiWriter(os.Stderr, socket),
			Dir:               buildPath,
			PTY:               runInPTY,
			InterruptSignal:   cancelSignal,
			SignalGracePeriod: signalGracePeriod,
		})

		// We aren't expecting the user to Ctrl-C the process (we're in a k8s
		// pod), but Kubernetes might send signals.
		// Forward them to the subprocess.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals,
			os.Interrupt,
			syscall.SIGHUP,
			syscall.SIGTERM,
			syscall.SIGINT,
			syscall.SIGQUIT,
		)

		go func() {
			defer signal.Stop(signals)
			// Forward signals to the subprocess.
			for {
				select {
				case <-ctx.Done():
					return
				case <-proc.Done():
					return
				case <-signals:
					proc.Interrupt()
				}
			}
		}()

		exitCode := -1
		defer func() { socket.Exit(exitCode) }()

		// NB: Run blocks until the subprocess exits.
		if err := proc.Run(ctx); err != nil {
			fmt.Fprintf(socket, "Couldn't execute bootstrap: %v\n", err)
			return &ExitError{1, err}
		}

		exitCode = proc.WaitStatus().ExitStatus()
		return &SilentExitError{code: exitCode}
	},
}
