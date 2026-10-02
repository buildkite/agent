package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
)

const cacheExecHelpDescription = `Usage:

    buildkite-agent cache exec --name <name> [options] -- <command> [args...]

Description:

Runs the given command if the named cache has no result for the current cache
key.

Example:

    $ buildkite-agent cache exec --name frontend_build -- vite build

With this cache configuration, vite build is skipped whenever the sources,
lockfile and vite config are unchanged:

    caches:
      - name: frontend_build
        cache_key:
          - frontend_build
          - { checksum: ["src/**", package-lock.json, vite.config.ts] }
        target_paths:
          - dist`

type CacheExecConfig struct {
	GlobalConfig
	APIConfig
	CacheConfig
	RedactedVars []string `cli:"redacted-vars" normalize:"list"`
}

var CacheExecCommand = &cli.Command{
	Name:        "exec",
	Usage:       "Runs a command, or restores its cached result",
	Description: cacheExecHelpDescription,
	Flags:       slices.Concat(globalFlags(), apiFlags(), cacheFlags(), []cli.Flag{RedactedVars}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[CacheExecConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "cache-exec")
		defer span.End()

		args := c.Args().Slice()
		if len(args) == 0 {
			return errors.New("no command given; usage: buildkite-agent cache exec --name <name> -- <command> [args...]")
		}
		if len(cfg.Names) != 1 {
			return fmt.Errorf("cache exec needs exactly one --name, got %d", len(cfg.Names))
		}

		command := cacheExecCommand(args)

		// Like any cache problem, missing configuration runs the command without the cache unless --cache-fail-on-error.
		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")
		if apiCfg.Token == "" {
			return cache.RunUncached(l, cfg.FailOnError, errors.New("an API token must be provided to use the cache"), os.Stdout, os.Stderr, command)
		}
		apiClient := api.NewClient(l, apiCfg)

		cacheConfigFile, err := resolveCacheConfigFile(cfg.CacheConfigFile)
		if err != nil {
			return cache.RunUncached(l, cfg.FailOnError, err, os.Stdout, os.Stderr, command)
		}

		// Variables exported in this step's command aren't known to the job's redactor (it rescans the environment only after hooks), so find them here.
		envNeedles, _, err := redact.NeedlesFromEnv(cfg.RedactedVars)
		if err != nil {
			return err
		}

		cacheCfg := cache.Config{
			Registry:        cfg.Registry,
			BucketURL:       cfg.BucketURL,
			CacheConfigFile: cacheConfigFile,
			Names:           cfg.Names,
			FailOnError:     cfg.FailOnError,
			Redact:          cacheExecRedactor(envNeedles),
		}
		return cache.RunExec(ctx, l, apiClient, cacheCfg, os.Stdout, os.Stderr, command)
	},
}

// cacheExecRedactor redacts envNeedles locally, then has the job executor redact the job's own secrets (e.g. from secret get or redactor add), which never leave it.
func cacheExecRedactor(envNeedles []string) cache.Redactor {
	return func(ctx context.Context, chunks []jobapi.OutputChunk) ([]jobapi.OutputChunk, error) {
		chunks, err := jobapi.RedactChunks(chunks, envNeedles)
		if err != nil {
			return nil, err
		}
		client, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("couldn't reach the Job API to redact the job's secrets: %w", err)
		}
		return client.Redact(ctx, chunks)
	}
}

// cacheExecWaitDelay is how long to wait, after the command exits, for processes it left running to close its output; a plain step doesn't wait for them at all.
const cacheExecWaitDelay = time.Second

// cacheExecCommand runs args on the given streams, failing with the exit status a shell would give: the command's own, 128+N if signal N killed it, or 127 if it can't be started.
func cacheExecCommand(args []string) cache.Command {
	return func(stdout, stderr io.Writer) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.WaitDelay = cacheExecWaitDelay

		// Cancellation signals reach the command via the job's process group; catch them so we outlive it and return its exit status.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(signals)

		err := cmd.Run()
		if errors.Is(err, exec.ErrWaitDelay) { // the command succeeded; only processes it left running still held its output
			return nil
		}
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return NewSilentExitError(128 + int(status.Signal()))
			}
			return NewSilentExitError(exitErr.ExitCode())
		}
		if err != nil && cmd.Process == nil {
			return NewExitError(127, err)
		}
		return err
	}
}
