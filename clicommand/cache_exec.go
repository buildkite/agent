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

		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")
		if apiCfg.Token == "" {
			return errors.New("an API token must be provided to use the cache")
		}
		apiClient := api.NewClient(l, apiCfg)

		cacheConfigFile, err := resolveCacheConfigFile(cfg.CacheConfigFile)
		if err != nil {
			return err
		}

		// The saved output is replayed in jobs that may lack these secrets, so redact it before saving.
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
			// Secrets from secret get and redactor add are only known to the job's redactor, so ask the Job API for them.
			Redactions: func(ctx context.Context) ([]string, error) {
				jobNeedles, err := listJobRedactions(ctx)
				if err != nil {
					return nil, fmt.Errorf("couldn't get the job's secrets from the Job API: %w", err)
				}
				return append(slices.Clone(envNeedles), jobNeedles...), nil
			},
		}
		return cache.RunExec(ctx, l, apiClient, cacheCfg, os.Stdout, os.Stderr, cacheExecCommand(args))
	},
}

// listJobRedactions returns the values the job log redacts, from the Job API.
func listJobRedactions(ctx context.Context) ([]string, error) {
	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.RedactionList(ctx)
}

// cacheExecCommand runs args on the given streams; a failure returns a SilentExitError with its exit status.
func cacheExecCommand(args []string) cache.Command {
	return func(stdout, stderr io.Writer) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		// Cancellation signals reach the command via the job's process group; catch them so we outlive it and return its exit status.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(signals)

		err := cmd.Run()
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			if code < 0 { // terminated by a signal
				code = 1
			}
			return NewSilentExitError(code)
		}
		return err
	}
}
