package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
	"os"
)

type AgentStartConfiguration struct {
	File                             string
	Token                            string   `cli:"token"`
	Name                             string   `cli:"name"`
	Priority                         string   `cli:"priority"`
	BootstrapScript                  string   `cli:"bootstrap-script"`
	BuildPath                        string   `cli:"build-path"`
	HooksPath                        string   `cli:"hooks-path"`
	MetaData                         []string `cli:"meta-data"`
	MetaDataEC2Tags                  bool     `cli:"meta-data-ec2-tags"`
	NoColor                          bool     `cli:"no-color"`
	NoAutoSSHFingerprintVerification bool     `cli:"no-automatic-ssh-fingerprint-verification"`
	NoCommandEval                    bool     `cli:"no-command-eval"`
	NoPTY                            bool     `cli:"no-pty"`
	Endpoint                         string   `cli:"endpoint"`
	Debug                            bool     `cli:"debug"`
}

func AgentStartCommandAction(c *cli.Context) {
	var configuration AgentStartConfiguration

	// Load the configuration
	err := buildkite.LoadConfiguration(&configuration, c)
	if err != nil {
		logger.Fatal("Failed to load configuration: %s", err)
	}

	// Toggle colors
	if configuration.NoColor {
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

	// then it's been loaded and we should show which one we loaded.
	if configuration.File != "" {
		logger.Info("Configuration loaded from: %s", configuration.File)
	}

	// Init debugging
	if configuration.Debug {
		logger.SetLevel(logger.DEBUG)
	}

	agentRegistrationToken := configuration.Token
	if agentRegistrationToken == "" {
		logger.Fatal("Missing --token. See 'buildkite-agent start --help'")
	}

	var agent buildkite.Agent

	// Expand the environment variable
	agent.BootstrapScript = os.ExpandEnv(configuration.BootstrapScript)
	if agent.BootstrapScript == "" {
		logger.Fatal("Bootstrap script is missing")
	}
	logger.Debug("Bootstrap script: %s", agent.BootstrapScript)

	// Just double check that the bootstrap script exists
	if _, err := os.Stat(agent.BootstrapScript); os.IsNotExist(err) {
		logger.Fatal("Could not find a bootstrap script located at: %s", agent.BootstrapScript)
	}

	// Expand the build path. We don't bother checking to see if it can be
	// written to, because it'll show up in the agent logs if it doesn't
	// work.
	agent.BuildPath = os.ExpandEnv(configuration.BuildPath)
	logger.Debug("Build path: %s", agent.BuildPath)

	// Expand the hooks path that is used by the bootstrap script
	agent.HooksPath = os.ExpandEnv(configuration.HooksPath)
	logger.Debug("Hooks directory: %s", agent.HooksPath)

	// Set the agents meta data
	agent.MetaData = configuration.MetaData

	// Should we try and grab the ec2 tags as well?
	if configuration.MetaDataEC2Tags {
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
	agent.Name = configuration.Name
	agent.Priority = configuration.Priority

	// Set auto fingerprint option
	agent.AutoSSHFingerprintVerification = !configuration.NoAutoSSHFingerprintVerification
	if !agent.AutoSSHFingerprintVerification {
		logger.Debug("Automatic SSH fingerprint verification has been disabled")
	}

	// Set script eval option
	agent.CommandEval = !configuration.NoCommandEval
	if !agent.CommandEval {
		logger.Debug("Evaluating console commands has been disabled")
	}

	agent.OS, _ = buildkite.MachineOSDump()
	agent.Hostname, _ = buildkite.MachineHostname()
	agent.Version = buildkite.Version()
	agent.PID = os.Getpid()

	// Toggle PTY
	if buildkite.MachineIsWindows() {
		agent.RunInPty = false
	} else {
		agent.RunInPty = !configuration.NoPTY

		if !agent.RunInPty {
			logger.Debug("Running builds within a pseudoterminal (PTY) has been disabled")
		}
	}

	logger.Info("Registering agent with Buildkite...")

	// Register the agent
	var registrationClient buildkite.Client
	registrationClient.AuthorizationToken = agentRegistrationToken
	registrationClient.URL = configuration.Endpoint
	err = registrationClient.AgentRegister(&agent)
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Successfully registered agent \"%s\" with meta-data %s", agent.Name, agent.MetaData)

	// Configure the agent's client
	agent.Client.AuthorizationToken = agent.AccessToken
	agent.Client.URL = configuration.Endpoint

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
