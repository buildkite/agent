package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/ec2"
	"github.com/buildkite/agent/buildkite/machine"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
	"os"
	"path/filepath"
)

var StartDescription = `Usage:

   buildkite-agent start [arguments...]

Description:

   When a job is ready to run it will call the "bootstrap-script"
   and pass it all the environment variables required for the job to run.
   This script is responsible for checking out the code, and running the
   actual build script defined in the project.

   The agent will run any jobs within a PTY (pseudo terminal) if available.

Example:

   $ buildkite-agent start --token xxx`

type AgentStartConfig struct {
	Config                           string   `cli:"config"`
	Token                            string   `cli:"token" validate:"required"`
	Name                             string   `cli:"name"`
	Priority                         string   `cli:"priority"`
	BootstrapScript                  string   `cli:"bootstrap-script" normalize:"filepath" validate:"required,file-exists"`
	BuildPath                        string   `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath                        string   `cli:"hooks-path" normalize:"filepath"`
	MetaData                         []string `cli:"meta-data"`
	MetaDataEC2Tags                  bool     `cli:"meta-data-ec2-tags"`
	NoColor                          bool     `cli:"no-color"`
	NoAutoSSHFingerprintVerification bool     `cli:"no-automatic-ssh-fingerprint-verification"`
	NoCommandEval                    bool     `cli:"no-command-eval"`
	NoPTY                            bool     `cli:"no-pty"`
	Endpoint                         string   `cli:"endpoint" validate:"required"`
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

var AgentStartCommand = cli.Command{
	Name:        "start",
	Usage:       "Starts a Buildkite agent",
	Description: StartDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "config",
			Value:  "",
			Usage:  "Path to a configration file",
			EnvVar: "BUILDKITE_AGENT_CONFIG",
		},
		cli.StringFlag{
			Name:   "token",
			Value:  "",
			Usage:  "Your account agent token",
			EnvVar: "BUILDKITE_AGENT_TOKEN",
		},
		cli.StringFlag{
			Name:   "name",
			Value:  "",
			Usage:  "The name of the agent",
			EnvVar: "BUILDKITE_AGENT_NAME",
		},
		cli.StringFlag{
			Name:   "priority",
			Value:  "",
			Usage:  "The priority of the agent (higher priorities are assigned work first)",
			EnvVar: "BUILDKITE_AGENT_PRIORITY",
		},
		cli.StringSliceFlag{
			Name:   "meta-data",
			Value:  &cli.StringSlice{},
			Usage:  "Meta data for the agent (default is \"queue=default\")",
			EnvVar: "BUILDKITE_AGENT_META_DATA",
		},
		cli.BoolFlag{
			Name:  "meta-data-ec2-tags",
			Usage: "Populate the meta data from the current instances EC2 Tags",
		},
		cli.StringFlag{
			Name:   "bootstrap-script",
			Value:  "",
			Usage:  "Path to the bootstrap script",
			EnvVar: "BUILDKITE_BOOTSTRAP_SCRIPT_PATH",
		},
		cli.StringFlag{
			Name:   "build-path",
			Value:  "",
			Usage:  "Path to where the builds will run from",
			EnvVar: "BUILDKITE_BUILD_PATH",
		},
		cli.StringFlag{
			Name:   "hooks-path",
			Value:  "",
			Usage:  "Directory where the hook scripts are found",
			EnvVar: "BUILDKITE_HOOKS_PATH",
		},
		cli.BoolFlag{
			Name:   "no-pty",
			Usage:  "Do not run jobs within a pseudo terminal",
			EnvVar: "BUILDKITE_NO_PTY",
		},
		cli.BoolFlag{
			Name:   "no-automatic-ssh-fingerprint-verification",
			Usage:  "Don't automatically verify SSH fingerprints",
			EnvVar: "BUILDKITE_NO_AUTOMATIC_SSH_FINGERPRINT_VERIFICATION",
		},
		cli.BoolFlag{
			Name:   "no-command-eval",
			Usage:  "Don't allow this agent to run arbitrary console commands",
			EnvVar: "BUILDKITE_NO_COMMAND_EVAL",
		},
		cli.StringFlag{
			Name:   "endpoint",
			Value:  DefaultEndpoint,
			Usage:  "The Agent API endpoint",
			EnvVar: "BUILDKITE_AGENT_ENDPOINT",
		},
		cli.BoolFlag{
			Name:   "debug",
			Usage:  "Enable debug mode",
			EnvVar: "BUILDKITE_AGENT_DEBUG",
		},
		cli.BoolFlag{
			Name:   "no-color",
			Usage:  "Don't show colors in logging",
			EnvVar: "BUILDKITE_AGENT_NO_COLOR",
		},
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := AgentStartConfig{}

		// Setup the config loader. You'll see that we also path paths to
		// potential config files. The loader will use the first one it finds.
		loader := cliconfig.Loader{
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

		var agent buildkite.Agent
		var err error

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
		agent.API.Token = cfg.Token

		// Register the agent
		if err := agent.Register(); err != nil {
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
	},
}
