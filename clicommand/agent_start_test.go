package clicommand_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/logger"
	"github.com/imdario/mergo"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

func setupHooksPath(t *testing.T) (string, func()) {
	hooksPath, err := os.MkdirTemp("", "")
	if err != nil {
		assert.FailNow(t, "failed to create temp file: %v", err)
	}
	return hooksPath, func() { os.RemoveAll(hooksPath) }
}

func writeAgentHook(t *testing.T, dir, hookName string) string {
	var filename, script string
	if runtime.GOOS == "windows" {
		filename = hookName + ".bat"
		script = "@echo off\necho hello world"
	} else {
		filename = hookName
		script = "echo hello world"
	}
	filepath := filepath.Join(dir, filename)
	if err := os.WriteFile(filepath, []byte(script), 0755); err != nil {
		assert.FailNow(t, "failed to write %q hook: %v", hookName, err)
	}
	return filepath
}

func TestAgentStartupHook(t *testing.T) {
	cfg := func(hooksPath string) clicommand.AgentStartConfig {
		return clicommand.AgentStartConfig{
			HooksPath: hooksPath,
			NoColor:   true,
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	t.Run("with agent-startup hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-startup")
		log := logger.NewBuffer()
		err := clicommand.AgentStartupHook(log, cfg(hooksPath))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{
				"[info] " + prompt + " " + filepath, // prompt
				"[info] hello world",                // output
			}, log.Messages)
		}
	})
	t.Run("with no agent-startup hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		err := clicommand.AgentStartupHook(log, cfg(hooksPath))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{}, log.Messages)
		}
	})
	t.Run("with bad hooks path", func(t *testing.T) {
		log := logger.NewBuffer()
		err := clicommand.AgentStartupHook(log, cfg("zxczxczxc"))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{}, log.Messages)
		}
	})
}

func TestAgentShutdownHook(t *testing.T) {
	cfg := func(hooksPath string) clicommand.AgentStartConfig {
		return clicommand.AgentStartConfig{
			HooksPath: hooksPath,
			NoColor:   true,
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	t.Run("with agent-shutdown hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-shutdown")
		log := logger.NewBuffer()
		clicommand.AgentShutdownHook(log, cfg(hooksPath))

		assert.Equal(t, []string{
			"[info] " + prompt + " " + filepath, // prompt
			"[info] hello world",                // output
		}, log.Messages)
	})
	t.Run("with no agent-shutdown hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		clicommand.AgentShutdownHook(log, cfg(hooksPath))
		assert.Equal(t, []string{}, log.Messages)
	})
	t.Run("with bad hooks path", func(t *testing.T) {
		log := logger.NewBuffer()
		clicommand.AgentShutdownHook(log, cfg("zxczxczxc"))
		assert.Equal(t, []string{}, log.Messages)
	})
}

func TestAgentStartCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []Test[any]{
		{
			name: "agent start",
			env:  map[string]string{},
			args: []string{"buildkite-agent", "start", "--token", "llamas"},
			expectedConfig: defaultAgentStartConfig(&clicommand.AgentStartConfig{
				Token: "llamas",
			}),
		},
		{
			name: "agent start git flags",
			env:  map[string]string{},
			args: []string{
				"buildkite-agent", "start", "--git-clone-flags", "-v --depth=1", "--git-fetch-flags", "-v --depth=1",
			},
			expectedConfig: defaultAgentStartConfig(&clicommand.AgentStartConfig{
				GitCloneFlags: "-v --depth=1",
				GitFetchFlags: "-v --depth=1",
				Token:         "xxx",
			}),
		},
		{
			name: "agent start git flags",
			env:  map[string]string{},
			args: []string{
				"buildkite-agent", "start", "--git-clone-flags=\"-v --depth=1\"", "--git-fetch-flags=\"-v --depth=1\"",
			},
			expectedConfig: defaultAgentStartConfig(&clicommand.AgentStartConfig{
				GitCloneFlags: "-v --depth=1",
				GitFetchFlags: "-v --depth=1",
				Token:         "xxx",
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test := test
			t.Parallel()
			app := cli.NewApp()
			app.Name = "buildkite-agent"
			app.Version = "1"
			app.Action = func(c *cli.Context) {
				t.Errorf("Error: %v", c.Args())
			}
			app.CommandNotFound = func(c *cli.Context, command string) {
				t.Errorf("Error: %s %v", command, c.Args())
			}

			for k, v := range test.env {
				t.Setenv(k, v)
			}

			testAction := &clicommand.AgentAction[clicommand.AgentStartConfig]{
				Action: func(
					ctx context.Context,
					c *cli.Context,
					l logger.Logger,
					loader cliconfig.Loader,
					cfg *clicommand.AgentStartConfig,
				) error {
					assert.Equal(t, test.expectedConfig, *cfg, "expected: %v, received: %v")
					return nil
				},
			}

			app.Commands = []cli.Command{
				clicommand.AgentStartCommand(ctx, testAction),
			}

			if err := app.Run(test.args); err != nil {
				t.Errorf("Error: %v", err)
			}
		})
	}
}

func defaultAgentStartConfig(cfg *clicommand.AgentStartConfig) clicommand.AgentStartConfig {
	defaultCfg := clicommand.AgentStartConfig{
		Config:                      "",
		Name:                        "%hostname-%spawn",
		Priority:                    "",
		AcquireJob:                  "",
		DisconnectAfterJob:          false,
		DisconnectAfterIdleTimeout:  0,
		BootstrapScript:             "",
		CancelGracePeriod:           10,
		EnableJobLogTmpfile:         false,
		BuildPath:                   "/var/lib/buildkite-agent/builds",
		HooksPath:                   "/etc/buildkite-agent/hooks",
		PluginsPath:                 "/etc/buildkite-agent/plugins",
		Shell:                       "/bin/bash -e -c",
		Tags:                        []string{},
		TagsFromEC2MetaData:         false,
		TagsFromEC2MetaDataPaths:    []string{},
		TagsFromEC2Tags:             false,
		TagsFromECSMetaData:         false,
		TagsFromGCPMetaData:         false,
		TagsFromGCPMetaDataPaths:    []string{},
		TagsFromGCPLabels:           false,
		TagsFromHost:                false,
		WaitForEC2TagsTimeout:       "10s",
		WaitForEC2MetaDataTimeout:   "10s",
		WaitForECSMetaDataTimeout:   "10s",
		WaitForGCPLabelsTimeout:     "10s",
		GitCloneFlags:               "-v",
		GitCloneMirrorFlags:         "-v",
		GitCleanFlags:               "-ffxdq",
		GitFetchFlags:               "-v --prune",
		GitMirrorsPath:              "",
		GitMirrorsLockTimeout:       300,
		GitMirrorsSkipUpdate:        false,
		NoGitSubmodules:             false,
		NoSSHKeyscan:                false,
		NoCommandEval:               false,
		NoLocalHooks:                false,
		NoPlugins:                   false,
		NoPluginValidation:          true,
		NoPTY:                       false,
		NoFeatureReporting:          false,
		TimestampLines:              false,
		HealthCheckAddr:             "",
		MetricsDatadog:              false,
		MetricsDatadogHost:          "127.0.0.1:8125",
		MetricsDatadogDistributions: false,
		TracingBackend:              "",
		TracingServiceName:          "buildkite-agent",
		Spawn:                       1,
		SpawnWithPriority:           false,
		LogFormat:                   "text",
		CancelSignal:                "SIGTERM",
		RedactedVars: []string{
			"*_PASSWORD",
			"*_SECRET",
			"*_TOKEN",
			"*_ACCESS_KEY",
			"*_SECRET_KEY",
		},
		Debug:                        false,
		LogLevel:                     "notice",
		NoColor:                      false,
		Experiments:                  []string{},
		Profile:                      "",
		DebugHTTP:                    false,
		Token:                        "",
		Endpoint:                     "https://agent.buildkite.com/v3",
		NoHTTP2:                      false,
		NoSSHFingerprintVerification: false,
		MetaData:                     []string{},
		MetaDataEC2:                  false,
		MetaDataEC2Tags:              false,
		MetaDataGCP:                  false,
		TagsFromEC2:                  false,
		TagsFromGCP:                  false,
		DisconnectAfterJobTimeout:    0,
	}

	if cfg != nil {
		_ = mergo.MergeWithOverwrite(&defaultCfg, cfg)
	}

	return defaultCfg
}
