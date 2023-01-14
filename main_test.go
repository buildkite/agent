package main_test

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/logger"
	"github.com/imdario/mergo"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

type Test[T any] struct {
	name           string
	env            map[string]string
	args           []string
	expectedConfig T
}

func TestCLICommands(t *testing.T) {
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
		BuildPath:                   "/tmp/buildkite-builds",
		HooksPath:                   "/tmp/buildkite-hooks",
		PluginsPath:                 "/tmp/buildkite-plugins",
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
		Endpoint:                     "http://agent.buildkite.localhost/v3",
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
		_ = mergo.Merge(&defaultCfg, cfg)
	}

	return defaultCfg
}
