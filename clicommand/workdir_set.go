package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

const workdirSetHelpDescription = `Usage:

    buildkite-agent workdir set <path>

Description:

Sets the working directory for subsequent phases of the current job. The change
persists across later hooks and the command phase.

This is intended for binary and polyglot (non-shell) hooks, which run in a child
process whose own directory changes are otherwise lost. Wrapped POSIX shell
hooks can change the working directory simply by ′cd′-ing.

Relative paths are resolved against the current working directory of this command
(i.e. the hook's actual working directory). The path must exist and be a
directory.

Examples:

Setting the working directory to a subdirectory of the checkout:

    $ buildkite-agent workdir set ./subdir

Setting the working directory to an absolute path:

    $ buildkite-agent workdir set /tmp/build-scratch`

type WorkdirSetConfig struct {
	GlobalConfig

	OutputFormat string `cli:"output-format"`
}

var WorkdirSetCommand = &cli.Command{
	Name:        "set",
	Usage:       "Sets the working directory for subsequent phases of the job",
	Description: workdirSetHelpDescription,
	Flags: append(globalFlags(),
		&cli.StringFlag{
			Name:    "output-format",
			Usage:   "Output format: quiet (no output), plain, or json",
			Sources: cli.EnvVars("BUILDKITE_AGENT_WORKDIR_SET_OUTPUT_FORMAT"),
			Value:   "plain",
		},
	),
	Action: workdirSetAction,
}

func workdirSetAction(ctx context.Context, c *cli.Command) error {
	ctx, cfg, _, _, done := setupLoggerAndConfig[WorkdirSetConfig](ctx, c)
	defer done()

	args := c.Args()
	if args.Len() != 1 {
		return fmt.Errorf("expected exactly one argument (the working directory), got %d", args.Len())
	}

	abs, err := resolveWorkdir(args.Get(0))
	if err != nil {
		return err
	}

	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return fmt.Errorf(envClientErrMessage, err)
	}

	workdir, err := client.SetWorkdir(ctx, abs)
	if err != nil {
		return fmt.Errorf("setting the job working directory: %w", err)
	}

	switch cfg.OutputFormat {
	case "quiet":
		return nil

	case "plain":
		_, _ = fmt.Fprintln(c.Writer, workdir)

	case "json":
		enc := json.NewEncoder(c.Writer)
		enc.SetEscapeHTML(false) // HTML escapes may interfere with secret redaction
		if err := enc.Encode(jobapi.WorkdirSetResponse{Workdir: workdir}); err != nil {
			return fmt.Errorf("error marshalling JSON: %w", err)
		}

	default:
		return fmt.Errorf("invalid output format %q", cfg.OutputFormat)
	}

	return nil
}

// resolveWorkdir resolves path to an absolute path against the current working
// directory (the hook's actual working directory) and validates that it exists
// and is a directory. The Job API endpoint only accepts absolute paths and does
// not stat the filesystem, so this resolution and validation happens client-side.
func resolveWorkdir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path of %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("checking working directory %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory %q is not a directory", abs)
	}

	return abs, nil
}
