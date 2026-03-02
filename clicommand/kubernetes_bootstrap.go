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
	"github.com/urfave/cli"
)

const kubernetesBootstrapHelpDescription = `Usage:

     buildkite-agent kubernetes-bootstrap [options...]

Description:

This command is used internally by Buildkite Kubernetes jobs. It is not
intended to be used directly.`

type KubernetesBootstrapConfig struct {
	KubernetesContainerID                int           `cli:"kubernetes-container-id"`
	KubernetesBootstrapConnectionTimeout time.Duration `cli:"kubernetes-bootstrap-connection-timeout"`

	// Global flags for debugging, etc
	LogLevel    string   `cli:"log-level"`
	Debug       bool     `cli:"debug"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var KubernetesBootstrapCommand = cli.Command{
	Name:        "kubernetes-bootstrap",
	Usage:       "Harness used internally by the agent to run jobs on Kubernetes",
	Category:    categoryInternal,
	Description: kubernetesBootstrapHelpDescription,
	Flags: []cli.Flag{
		KubernetesContainerIDFlag,
		cli.DurationFlag{
			Name: "kubernetes-bootstrap-connection-timeout",
			Usage: "This is intended to be used only by the Buildkite k8s stack " +
				"(github.com/buildkite/agent-stack-k8s); it set the max time a container will wait " +
				"to connect Agent.",
			EnvVar: "BUILDKITE_KUBERNETES_BOOTSTRAP_CONNECTION_TIMEOUT",
		},

		// Global flags for debugging, etc
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		// kubernetes-bootstrap first register with the agent server container (the container that runs `buildkite-agent start`)
		// As part the process, it will gain a bunch of env vars.
		// After registration, it will run `buildkite-agent bootstrap`
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
		connectionTimeout := 120 * time.Second
		if cfg.KubernetesBootstrapConnectionTimeout > 0 {
			connectionTimeout = cfg.KubernetesBootstrapConnectionTimeout
		}
		connectCtx, connectCancel := context.WithTimeout(ctx, connectionTimeout)
		defer connectCancel()
		regResp, err := socket.Connect(connectCtx)
		if err != nil {
			return fmt.Errorf("error connecting to kubernetes runner: %w", err)
		}
		defer socket.Close()

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
		cancelGracePeriodSecs := environ.GetInt("BUILDKITE_CANCEL_GRACE_PERIOD", defaultCancelGracePeriodSecs)
		cancelGracePeriod := time.Duration(cancelGracePeriodSecs) * time.Second
		signalGracePeriodSecs := environ.GetInt("BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS", defaultSignalGracePeriodSecs)
		signalGracePeriod, err := signalGracePeriod(cancelGracePeriodSecs, signalGracePeriodSecs)
		if err != nil {
			return err
		}

		// BUILDKITE_KUBERNETES_EXEC is a legacy environment variable. It was used to activate the socket
		// on the bootstrap command, and to activate the socket server on `buildkite-agent start`.
		// The former has been superseded by this `kubernetes-bootstrap` command.
		// We keep this env var because some users depend on it as a k8s environment detection mechanism.
		environ.Set("BUILDKITE_KUBERNETES_EXEC", "true")

		if _, exists := environ.Get("BUILDKITE_BUILD_CHECKOUT_PATH"); !exists {
			// The OG agent runs as a long-live worker, therefore it set a checkout path dynamically to cater
			// for different workloads.
			// The path can gets really long because Agent name contain auto generated uuid, it might break some customers'
			// use case.
			// The k8s agent runs emphemerally, there is no need to carefully craft a checkout path.
			environ.Set("BUILDKITE_BUILD_CHECKOUT_PATH", filepath.Join(buildPath, "buildkite"))
		}

		// For k8s agents, use shared plugin paths by default since each pod runs ephemerally.
		// This enables plugin caching across jobs when using persistent storage.
		if _, exists := environ.Get("BUILDKITE_PLUGINS_PATH_INCLUDES_AGENT_NAME"); !exists {
			environ.Set("BUILDKITE_PLUGINS_PATH_INCLUDES_AGENT_NAME", "false")
		}

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
				l.Error("kubernetes-bootstrap: Error waiting for client interrupt: %v; cancelling work", err)
			} else {
				l.Warn("kubernetes-bootstrap: Either the job was cancelled or the pod is being deleted; cancelling work")
			}
			// The context cancellation handler in process.Run first calls
			// Interrupt, waits for its signalGracePeriod, and then calls
			// Terminate.
			cancel()
			// If we block the StatusLoop goroutine, the client will be
			// considered missing after a short while.
			go func() {
				// If we're cancelling because the job was cancelled in the UI, we
				// should self-exit after cancelGracePeriod to be sure.
				// (If we're cancelling because the pod is being deleted, Kubernetes
				// enforces it after terminationGracePeriodSeconds, so self-exiting
				// in that case is superfluous.)
				time.Sleep(cancelGracePeriod)
				// We get here if the main goroutine hasn't returned yet.
				l.Info("kubernetes-bootstrap: Timed out waiting for subprocess to exit; exiting immediately with status 1")
				os.Exit(1)
			}()
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

		// We aren't expecting the user to Ctrl-C the process (we're in k8s),
		// but Kubernetes might send signals.
		// All the containers in the pod get SIGTERM when the pod is deleted,
		// followed up by SIGKILL after ~TerminationGracePeriodSeconds.
		// Instead of forwarding Kubernetes's SIGTERM to the subprocess
		// ourselves, we'll instead swallow the signals, and wait until the
		// agent container interrupts us via the Unix socket.
		signals := make(chan os.Signal, 1)
		signal.Notify(
			signals,
			os.Interrupt,
			syscall.SIGHUP,
			syscall.SIGTERM,
			syscall.SIGINT,
			syscall.SIGQUIT,
		)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-proc.Done():
					return
				case sig := <-signals:
					// Log but otherwise swallow the signal
					l.Info("kubernetes-bootstrap: Received %v; awaiting interrupt from agent", sig)
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
