package run

import (
	"os"
	"os/exec"
	"strings"

	"github.com/buildkite/agent/logger"
)

type CommandExecutor struct {
	Command []string
	Plugins []Plugin

	BuildPath       string
	PluginPath      string
	BootstrapScript string
}

func (c *CommandExecutor) Execute() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	commit, err := gitCommit()
	if err != nil {
		return err
	}

	branch, err := gitBranch()
	if err != nil {
		return err
	}

	plugins, err := marshalPlugins(c.Plugins)
	if err != nil {
		return err
	}

	logger.Debug("Executing buildkite-agent bootstrap")
	cmd := exec.Command("buildkite-agent", "bootstrap")

	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	cmd.Env = append(os.Environ(),
		`BUILDKITE_BUILD_PATH=`+c.BuildPath,
		`BUILDKITE_PLUGINS_PATH=`+c.PluginPath,
		`BUILDKITE_AGENT_NAME=local`,
		`BUILDKITE_BUILD_NUMBER=42`,
		`BUILDKITE_JOB_ID=42`,
		`BUILDKITE_COMMAND=`+strings.Join(c.Command, "\n"),
		`BUILDKITE_ORGANIZATION_SLUG=local`,
		`BUILDKITE_PIPELINE_SLUG=`+wd,
		`BUILDKITE_PIPELINE_PROVIDER=local`,
		`BUILDKITE_REPO=`+wd,
		`BUILDKITE_COMMIT=`+commit,
		`BUILDKITE_BRANCH=`+branch,
		`BUILDKITE_PLUGINS=`+plugins,
	)

	return cmd.Run()
}

func gitCommit() (string, error) {
	out, err := exec.Command(`git`, `rev-parse`, `--abbrev-ref`, `HEAD`).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

func gitBranch() (string, error) {
	out, err := exec.Command(`git`, `rev-parse`, `HEAD`).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}
