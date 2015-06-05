package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
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
			Usage:  "Path to a configuration file",
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
		EndpointFlag,
		DebugFlag,
		NoColorFlag,
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
		HandleGlobalFlags(cfg)

		// Setup the agent
		runner := buildkite.AgentRunner{
			API: buildkite.API{
				Endpoint: cfg.Endpoint,
				Token:    cfg.Token,
			},
			Name:                             cfg.Name,
			Priority:                         cfg.Priority,
			BootstrapScript:                  cfg.BootstrapScript,
			BuildPath:                        cfg.BuildPath,
			HooksPath:                        cfg.HooksPath,
			MetaData:                         cfg.MetaData,
			MetaDataEC2Tags:                  cfg.MetaDataEC2Tags,
			NoAutoSSHFingerprintVerification: cfg.NoAutoSSHFingerprintVerification,
			NoCommandEval:                    cfg.NoCommandEval,
			NoPTY:                            cfg.NoPTY,
		}

		// Store the loaded config file path on the runner so we can
		// show it when the agent starts
		if loader.File != nil {
			runner.ConfigFilePath = loader.File.Path
		}

		// Run the agent
		if err := runner.Run(); err != nil {
			logger.Fatal("%s", err)
		}
	},
}
