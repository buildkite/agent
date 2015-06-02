package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	config2 "github.com/buildkite/agent/buildkite/config"
	"github.com/buildkite/agent/buildkite/ec2"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/buildkite/agent/buildkite/machine"
	"github.com/codegangsta/cli"
	"os"
	"path/filepath"
)

type AgentStartConfig struct {
	Config                           string   `cli:"config"`
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

func DefaultConfigFilePaths() (paths []string) {
	// Toggle beetwen windows an *nix paths
	if machine.IsWindows() {
		paths = []string{
			"$USERPROFILE\\AppData\\Local\\BuildkiteAgent\\buildkite-agent.cfg",
		}
	} else {
		paths = []string{
			"$HOME/.buildkite-agent/buildkite-agent.cfg",
			"/usr/local/etc/buildkite-agent/buildkite-agent.cfg",
			"/etc/buildkite-agent/buildkite-agent.cfg",
		}
	}

	// Also check to see if there's a buildkite-agent.cfg in the folder
	// that the binary is running in.
	pathToBinary, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err == nil {
		pathToRelativeConfig := filepath.Join(pathToBinary, "buildkite-agent.cfg")
		paths = append([]string{pathToRelativeConfig}, paths...)
	}

	return
}

func AgentStartCommandAction(c *cli.Context) {
	// The configuration will be loaded into this struct
	cfg := AgentStartConfig{}

	// Setup the config loader. You'll see that we also path paths to
	// potential config files. The loader will use the first one it finds.
	loader := config2.Loader{
		CLI:                    c,
		Config:                 &cfg,
		DefaultConfigFilePaths: DefaultConfigFilePaths(),
	}

	// Load the configuration
	if err := loader.Load(); err != nil {
		logger.Fatal("%s", err)
	}

	// Setup the any global configuration options
	SetupGlobalConfiguration(cfg)

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
	if loader.File != nil {
		logger.Info("Configuration loaded from: %s", loader.File.Path)
	}

	agentRegistrationToken := cfg.Token
	if agentRegistrationToken == "" {
		logger.Fatal("Missing --token. See 'buildkite-agent start --help'")
	}

	var agent buildkite.Agent
	var err error

	// Expand the environment variable
	agent.BootstrapScript = os.ExpandEnv(cfg.BootstrapScript)
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
	agent.BuildPath = os.ExpandEnv(cfg.BuildPath)
	logger.Debug("Build path: %s", agent.BuildPath)

	// Expand the hooks path that is used by the bootstrap script
	agent.HooksPath = os.ExpandEnv(cfg.HooksPath)
	logger.Debug("Hooks directory: %s", agent.HooksPath)

	// Set the agents meta data
	agent.MetaData = cfg.MetaData

	// Should we try and grab the ec2 tags as well?
	if cfg.MetaDataEC2Tags {
		tags, err := ec2.GetTags()

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
	agent.Name = cfg.Name
	agent.Priority = cfg.Priority

	// Set auto fingerprint option
	agent.AutoSSHFingerprintVerification = !cfg.NoAutoSSHFingerprintVerification
	if !agent.AutoSSHFingerprintVerification {
		logger.Debug("Automatic SSH fingerprint verification has been disabled")
	}

	// Set script eval option
	agent.CommandEval = !cfg.NoCommandEval
	if !agent.CommandEval {
		logger.Debug("Evaluating console commands has been disabled")
	}

	agent.Hostname, err = machine.Hostname()
	if err != nil {
		logger.Fatal("Could not retrieve hostname: %s", err)
	}

	agent.OS, _ = machine.OSDump()
	agent.Version = buildkite.Version()
	agent.PID = os.Getpid()

	// Toggle PTY
	if machine.IsWindows() {
		agent.RunInPty = false
	} else {
		agent.RunInPty = !cfg.NoPTY

		if !agent.RunInPty {
			logger.Debug("Running builds within a pseudoterminal (PTY) has been disabled")
		}
	}

	logger.Info("Registering agent with Buildkite...")

	// Send the Buildkite API endpoint
	agent.API.Endpoint = cfg.Endpoint

	// Use the registartion token as the token
	agent.API.Token = agentRegistrationToken

	// Register the agent
	err = agent.Register(cfg.Endpoint, agentRegistrationToken)
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Successfully registered agent \"%s\" with meta-data %s", agent.Name, agent.MetaData)

	// Configure the agent's client (legacy)
	agent.Client.AuthorizationToken = agent.AccessToken
	agent.Client.URL = cfg.Endpoint

	// Now we can switch to the Agents API access token
	agent.API.Token = agent.AccessToken

	// Setup signal monitoring
	agent.MonitorSignals()

	// Connect the agent
	logger.Info("Connecting to Buildkite...")
	err = agent.Connect()
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Agent successfully connected")
	logger.Info("You can press Ctrl-C to stop the agent")
	logger.Info("Waiting for work...")

	// Start the agent
	agent.Start()
}
