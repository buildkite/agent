package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
	"os"
	"time"
)

func AgentStartCommandAction(c *cli.Context) {
	// For display purposes, come up with what the name of the agent is.
	// agentName, err := buildkite.MachineHostname()
	// if c.String("name") != "" {
	// 	agentName = c.String("name")
	// }

	// Toggle colors
	if c.Bool("no-color") {
		logger.SetColors(false)
	}

	welcomeMessage :=
		"\n" +
			"\x1b[32m  _           _ _     _ _    _ _                                _\n" +
			" | |         (_) |   | | |  (_) |                              | |\n" +
			" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
			" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
			" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
			" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
			"                                                __/ |\n" +
			" http://buildkite.com/agent                    |___/\n\x1b[0m\n"

	fmt.Printf(welcomeMessage)

	logger.Notice("Starting buildkite-agent v%s with PID: %s", buildkite.Version(), fmt.Sprintf("%d", os.Getpid()))
	logger.Notice("Copyright (c) 2014-%d, Buildkite Pty Ltd. See LICENSE and for more details.", time.Now().Year())
	logger.Notice("For questions and support, email us at: hello@buildkite.com")

	// Init debugging
	if c.Bool("debug") {
		logger.SetLevel(logger.DEBUG)
	}

	// Create the agent
	if c.String("access-token") != "" {
		fmt.Println("buildkite-agent: use of --access-token is now deprecated\nSee 'buildkite-agent start --help'")
		os.Exit(1)
	}

	agentRegistrationToken := c.String("token")
	if agentRegistrationToken == "" {
		fmt.Println("buildkite-agent: missing token\nSee 'buildkite-agent start --help'")
		os.Exit(1)
	}

	// Expand the envionment variable.
	bootstrapScript := os.ExpandEnv(c.String("bootstrap-script"))

	// Make sure the boostrap script exists.
	if _, err := os.Stat(bootstrapScript); os.IsNotExist(err) {
		fmt.Printf("buildkite-agent: could not find bootstrap script %s\n", bootstrapScript)
		os.Exit(1)
	}

	logger.Debug("Bootstrap script: %s", bootstrapScript)

	// Expand the build path. We don't bother checking to see if it can be
	// written to, because it'll show up in the agent logs if it doesn't
	// work.
	buildPath := os.ExpandEnv(c.String("build-path"))
	logger.Debug("Build path: %s", buildPath)

	// Expand the hooks path that is used by the bootstrap script
	hooksPath := os.ExpandEnv(c.String("hooks-path"))
	logger.Debug("Hooks directory: %s", hooksPath)

	// Create a client so we can register the agent
	var client buildkite.Client
	client.AuthorizationToken = agentRegistrationToken
	client.URL = c.String("endpoint")

	agentMetaData := c.StringSlice("meta-data")

	// Should we try and grab the ec2 tags as well?
	if c.Bool("meta-data-ec2-tags") {
		tags, err := buildkite.EC2InstanceTags()

		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			logger.Error(fmt.Sprintf("Failed to find EC2 Tags: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agentMetaData = append(agentMetaData, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Set the agent options
	var agent buildkite.Agent
	agent.BootstrapScript = bootstrapScript
	agent.BuildPath = buildPath
	agent.HooksPath = hooksPath

	if buildkite.MachineIsWindows() {
		agent.RunInPty = false
	} else {
		agent.RunInPty = !c.Bool("no-pty")
	}

	agent.AutoSSHFingerprintVerification = !c.Bool("no-automatic-ssh-fingerprint-verification")
	agent.ScriptEval = !c.Bool("no-script-eval")

	if !agent.ScriptEval {
		logger.Info("Evaluating scripts has been disabled for this agent")
	}

	// Register the agent
	agentAccessToken, err := client.AgentRegister(c.String("name"), c.String("priority"), agentMetaData, agent.ScriptEval)
	if err != nil {
		logger.Fatal("%s", err)
	}

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("endpoint")

	// Setup the agent
	agent.Setup()

	// Setup signal monitoring
	agent.MonitorSignals()

	// Start the agent
	agent.Start()
}
