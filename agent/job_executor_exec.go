package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/job"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/shellwords"
)

// execExecutor runs bootstrap-script as a local subprocess of the agent.
type execExecutor struct {
	logger            logger.Logger
	bootstrapScript   string
	buildPath         string
	runInPty          bool
	cancelSignal      process.Signal
	signalGracePeriod time.Duration
}

func (e *execExecutor) New(_ context.Context, req JobExecutionRequest) (JobExecution, error) {
	// The bootstrap-script gets parsed based on the operating system
	cmd, err := shellwords.Split(e.bootstrapScript)
	if err != nil {
		return nil, fmt.Errorf("splitting bootstrap-script (%q) into tokens: %w", e.bootstrapScript, err)
	}

	// CancelSignal == SIGKILL means the user wants the command to be killed
	// instead of signaled more gracefully (SIGTERM, SIGINT, etc).
	// We don't send SIGKILL to the bootstrap itself as a cancel signal,
	// because that would kill the bootstrap immediately, which would
	// prevent capturing the exit status of the command, executing various
	// pre-exit hooks, and other cleanup.
	cancelSignal := e.cancelSignal
	if cancelSignal == process.SIGKILL {
		cancelSignal = process.SIGTERM
	}

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	env := append(os.Environ(), req.Env...)

	return execExecution{
		Process: process.New(e.logger, process.Config{
			Path:              cmd[0],
			Args:              cmd[1:],
			Dir:               e.buildPath,
			Env:               env,
			PTY:               e.runInPty,
			Stdout:            req.Output,
			Stderr:            req.Output,
			InterruptSignal:   cancelSignal,
			SignalGracePeriod: e.signalGracePeriod,
		}),
		internalTracingExporter: req.InternalTracingExporter,
		logger:                  e.logger,
	}, nil
}

// execExecution is a local bootstrap subprocess. Run closes any private pipe.
type execExecution struct {
	*process.Process
	internalTracingExporter *api.TracingExporter
	logger                  logger.Logger
}

func (e execExecution) Run(ctx context.Context) error {
	if e.internalTracingExporter == nil {
		return e.Process.Run(ctx)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		e.logger.Warnf("Could not create internal tracing pipe")
		return e.Process.Run(ctx)
	}
	defer reader.Close() //nolint:errcheck // Also release the writer if bootstrap fails or exits without reading.
	defer writer.Close() //nolint:errcheck // Best-effort telemetry.
	go func() {
		defer writer.Close() //nolint:errcheck // Bootstrap reads until EOF.
		// Write while bootstrap reads; configuration can exceed the pipe buffer.
		_ = json.NewEncoder(writer).Encode(e.internalTracingExporter)
	}()
	return e.RunWithFile(ctx, job.InternalTracingExporterEnv, reader)
}

func (execExecution) Cleanup(context.Context) error { return nil }
