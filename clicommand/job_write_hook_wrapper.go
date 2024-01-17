package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/internal/job/hook"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

type JobWriteHookWrapperConfig struct {
	ShebangLine   string `cli:"shebang-line"`
	BeforeEnvFile string `cli:"before-env-file"`
	AfterEnvFile  string `cli:"after-env-file"`
	HookFile      string `cli:"hook-file"`

	HookWrapperName string `cli:"arg:0" label:"hook-wrapper-name"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var ErrNoHookWrapperName = errors.New("hook-wrapper-name is required")

var JobWriteHookWrapperCommand = cli.Command{
	Name:  "write-hook-wrapper",
	Usage: "Write the hook wrapper file",
	Description: `Usage:

    buildkite-agent job write-hook-wrapper [options...] <hook-wrapper-name>

Description:

This command is intended to be used internally by the agent.

Write the hook wrapper file to a temporary file based on the given name. This is
an executable file that will the hook, when the hook is a shell script. The
agent will run this script instead of the hook directly. This is part of a
mechanism that allows environment changes in the hook to be propogated to latter
hooks.

This command prints the path of the hook wrapper to stdout. The process calling
this command is responsible for deleting the file when it is no longer needed.`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:     "shebang-line",
			Usage:    "The shebang line to use in the script wrapper file.",
			EnvVar:   "BUILDKITE_JOB_SCRIPT_WRAPPER_SHEBANG_LINE",
			Required: true,
		},
		cli.StringFlag{
			Name:     "before-env-file",
			Usage:    "The path to the file to write the environment to before the hook is run.",
			EnvVar:   "BUILDKITE_JOB_SCRIPT_WRAPPER_BEFORE_ENV_FILE",
			Required: true,
		},
		cli.StringFlag{
			Name:     "after-env-file",
			Usage:    "The path to the file to write the environment to after the hook is run.",
			EnvVar:   "BUILDKITE_JOB_SCRIPT_WRAPPER_AFTER_ENV_FILE",
			Required: true,
		},
		cli.StringFlag{
			Name:     "hook-file",
			Usage:    "The path to the hook file to source in the hook wrapper.",
			EnvVar:   "BUILDKITE_JOB_SCRIPT_WRAPPER_HOOK_FILE",
			Required: true,
		},

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		_, cfg, l, _, done := setupLoggerAndConfig[JobWriteHookWrapperConfig](context.Background(), c)
		defer done()

		if cfg.HookWrapperName == "" {
			return ErrNoHookWrapperName
		}

		l = l.WithFields(logger.StringField("hook-wrapper-name", cfg.HookWrapperName))
		l.Debug("Writing script wrapper file")

		executable, err := os.Executable()
		if err != nil {
			return err
		}

		name, err := hook.WriteHookWrapper(
			hook.PosixShellTemplateType,
			hook.WrapperTemplateInput{
				AgentBinary:       executable,
				ShebangLine:       cfg.ShebangLine,
				BeforeEnvFileName: cfg.BeforeEnvFile,
				AfterEnvFileName:  cfg.AfterEnvFile,
				PathToHook:        cfg.HookFile,
			},
			cfg.HookWrapperName,
		)
		if err != nil {
			return err
		}

		if _, err = fmt.Fprintln(c.App.Writer, name); err != nil {
			return err
		}

		return nil
	},
}
