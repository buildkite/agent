package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
	"os"
)

func AgentStartCommandAction(c *cli.Context) {
	// Toggle colors
	if c.Bool("no-color") {
		logger.SetColors(false)
	}

	welcomeMessage :=
		"\n" +
			"%s  _           _ _     _ _    _ _                                _\n" +
			" | |         (_) |   | | |  (_) |                              | |\n" +
			" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
			" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
			" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
			" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
			"                                                __/ |\n" +
			" http://buildkite.com/agent                    |___/\n%s\n"

	// Don't do colors on the banner if they aren't enabled in the logger
	if logger.ColorsEnabled() {
		fmt.Fprintf(logger.OutputPipe(), welcomeMessage, "\x1b[32m", "\x1b[0m")
	} else {
		fmt.Fprintf(logger.OutputPipe(), welcomeMessage, "", "")
	}

	logger.Notice("Starting buildkite-agent v%s with PID: %s", buildkite.Version(), fmt.Sprintf("%d", os.Getpid()))
	logger.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	logger.Notice("For questions and support, email us at: hello@buildkite.com")

	// Init debugging
	if c.Bool("debug") {
		logger.SetLevel(logger.DEBUG)
	}

	agentRegistrationToken := c.String("token")
	if agentRegistrationToken == "" {
		logger.Fatal("Missing --token. See 'buildkite-agent start --help'")
	}

	var agent buildkite.Agent

	// Expand the envionment variable
	agent.BootstrapScript = os.ExpandEnv(c.String("bootstrap-script"))
	logger.Debug("Bootstrap script: %s", agent.BootstrapScript)

	// Just double check that the bootstrap script exists
	if _, err := os.Stat(agent.BootstrapScript); os.IsNotExist(err) {
		logger.Fatal("Could not find a bootstrap script located at: %s", agent.BootstrapScript)
	}

	// Expand the build path. We don't bother checking to see if it can be
	// written to, because it'll show up in the agent logs if it doesn't
	// work.
	agent.BuildPath = os.ExpandEnv(c.String("build-path"))
	logger.Debug("Build path: %s", agent.BuildPath)

	// Expand the hooks path that is used by the bootstrap script
	agent.HooksPath = os.ExpandEnv(c.String("hooks-path"))
	logger.Debug("Hooks directory: %s", agent.HooksPath)

	// Set the agents meta data
	agent.MetaData = c.StringSlice("meta-data")

	// Should we try and grab the ec2 tags as well?
	if c.Bool("meta-data-ec2-tags") {
		tags, err := buildkite.EC2InstanceTags()

		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			logger.Error(fmt.Sprintf("Failed to find EC2 Tags: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agent.MetaData = append(agent.MetaData, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// More CLI options
	agent.Name = c.String("name")
	agent.Priority = c.String("priority")

	// Set auto fingerprint option
	agent.AutoSSHFingerprintVerification = !c.Bool("no-automatic-ssh-fingerprint-verification")
	if !agent.AutoSSHFingerprintVerification {
		logger.Debug("Automatic SSH fingerprint verification has been disabled")
	}

	// Set script eval option
	agent.ScriptEval = !c.Bool("no-script-eval")
	if !agent.ScriptEval {
		logger.Debug("Evaluating scripts has been disabled")
	}

	agent.OS, _ = buildkite.MachineOSDump()
	agent.Hostname, _ = buildkite.MachineHostname()
	agent.Version = buildkite.Version()
	agent.PID = os.Getpid()

	// Toggle PTY
	if buildkite.MachineIsWindows() {
		agent.RunInPty = false
	} else {
		agent.RunInPty = !c.Bool("no-pty")

		if !agent.RunInPty {
			logger.Debug("Running builds within a pseudoterminal (PTY) has been disabled")
		}
	}

	logger.Info("Registering agent with Buildkite...")

	// Register the agent
	var registrationClient buildkite.Client
	registrationClient.AuthorizationToken = agentRegistrationToken
	registrationClient.URL = c.String("endpoint")
	err := registrationClient.AgentRegister(&agent)
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Successfully registred agent \"%s\" with meta-data %s", agent.Name, agent.MetaData)

	// Configure the agent's client
	agent.Client.AuthorizationToken = agent.AccessToken
	agent.Client.URL = c.String("endpoint")

	// Setup signal monitoring
	agent.MonitorSignals()

	// Connect the agent
	logger.Info("Connecting to Buildkite...")
	err = agent.Client.AgentConnect(&agent)
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Agent successfully connected")
	logger.Info("You can press Ctrl-C to stop the agent")
	logger.Info("Waiting for work...")

	// Start the agent
	agent.Start()
}
