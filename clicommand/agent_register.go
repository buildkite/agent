package clicommand

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/urfave/cli"
)

var RegisterDescription = `Usage:

   buildkite-agent register [arguments...]

Description:

   Register an Agent with the Buildkite API for advanced usage.

   The command outputs JSON to stdout that can be consumed by
   the 'buildkite-agent start' command.

Example:

   $ buildkite-agent register --token xxx`

type AgentRegisterConfig struct {
	Config                  string   `cli:"config"`
	Name                    string   `cli:"name"`
	Priority                string   `cli:"priority"`
	Tags                    []string `cli:"tags" normalize:"list"`
	TagsFromEC2             bool     `cli:"tags-from-ec2"`
	TagsFromEC2Tags         bool     `cli:"tags-from-ec2-tags"`
	TagsFromGCP             bool     `cli:"tags-from-gcp"`
	TagsFromGCPLabels       bool     `cli:"tags-from-gcp-labels"`
	TagsFromHost            bool     `cli:"tags-from-host"`
	WaitForEC2TagsTimeout   string   `cli:"wait-for-ec2-tags-timeout"`
	WaitForGCPLabelsTimeout string   `cli:"wait-for-gcp-labels-timeout"`
	NoCommandEval           bool     `cli:"no-command-eval"`

	// Global flags
	Debug           bool     `cli:"debug"`
	DebugWithoutAPI bool     `cli:"debug-without-api"`
	NoColor         bool     `cli:"no-color"`
	Experiments     []string `cli:"experiment" normalize:"list"`

	// API config
	DebugHTTP bool   `cli:"debug-http"`
	Token     string `cli:"token" validate:"required"`
	Endpoint  string `cli:"endpoint" validate:"required"`
	NoHTTP2   bool   `cli:"no-http2"`

	// Deprecated
	NoSSHFingerprintVerification bool     `cli:"no-automatic-ssh-fingerprint-verification" deprecated-and-renamed-to:"NoSSHKeyscan"`
	MetaData                     []string `cli:"meta-data" deprecated-and-renamed-to:"Tags"`
	MetaDataEC2                  bool     `cli:"meta-data-ec2" deprecated-and-renamed-to:"TagsFromEC2"`
	MetaDataEC2Tags              bool     `cli:"meta-data-ec2-tags" deprecated-and-renamed-to:"TagsFromEC2Tags"`
	MetaDataGCP                  bool     `cli:"meta-data-gcp" deprecated-and-renamed-to:"TagsFromGCP"`
}

var AgentRegisterCommand = cli.Command{
	Name:        "register",
	Usage:       "Register an Agent with the Buildkite API (advanced)",
	Description: RegisterDescription,
	Hidden:      true,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "config",
			Value:  "",
			Usage:  "Path to a configuration file",
			EnvVar: "BUILDKITE_AGENT_CONFIG",
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
			Name:   "tags",
			Value:  &cli.StringSlice{},
			Usage:  "A comma-separated list of tags for the agent (e.g. \"linux\" or \"mac,xcode=8\")",
			EnvVar: "BUILDKITE_AGENT_TAGS",
		},
		cli.BoolFlag{
			Name:   "tags-from-host",
			Usage:  "Include tags from the host (hostname, machine-id, os)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_HOST",
		},
		cli.BoolFlag{
			Name:   "tags-from-ec2",
			Usage:  "Include the host's EC2 meta-data as tags (instance-id, instance-type, and ami-id)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2",
		},
		cli.BoolFlag{
			Name:   "tags-from-ec2-tags",
			Usage:  "Include the host's EC2 tags as tags",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS",
		},
		cli.BoolFlag{
			Name:   "tags-from-gcp",
			Usage:  "Include the host's Google Cloud instance meta-data as tags (instance-id, machine-type, preemptible, project-id, region, and zone)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP",
		},
		cli.BoolFlag{
			Name:   "tags-from-gcp-labels",
			Usage:  "Include the host's Google Cloud instance labels as tags",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP_LABELS",
		},
		cli.DurationFlag{
			Name:   "wait-for-ec2-tags-timeout",
			Usage:  "The amount of time to wait for tags from EC2 before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_EC2_TAGS_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.DurationFlag{
			Name:   "wait-for-gcp-labels-timeout",
			Usage:  "The amount of time to wait for labels from GCP before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_GCP_LABELS_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.BoolFlag{
			Name:   "no-command-eval",
			Usage:  "Don't allow this agent to run arbitrary console commands, including plugins",
			EnvVar: "BUILDKITE_NO_COMMAND_EVAL",
		},

		// API Flags
		AgentRegisterTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,
		DebugWithoutAPIFlag,

		// Global flags
		ExperimentsFlag,
		NoColorFlag,
		DebugFlag,
	},
	Action: func(c *cli.Context) {
		l := logger.NewLogger()

		// The configuration will be loaded into this struct
		cfg := AgentRegisterConfig{}

		// Setup the config loader. You'll see that we also path paths to
		// potential config files. The loader will use the first one it finds.
		loader := cliconfig.Loader{
			CLI:                    c,
			Config:                 &cfg,
			DefaultConfigFilePaths: DefaultConfigFilePaths(),
			Logger:                 l,
		}

		// Load the configuration
		if err := loader.Load(); err != nil {
			l.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(l, cfg)

		var ec2TagTimeout time.Duration
		if t := cfg.WaitForEC2TagsTimeout; t != "" {
			var err error
			ec2TagTimeout, err = time.ParseDuration(t)
			if err != nil {
				l.Fatal("Failed to parse ec2 tag timeout: %v", err)
			}
		}

		var gcpLabelsTimeout time.Duration
		if t := cfg.WaitForGCPLabelsTimeout; t != "" {
			var err error
			gcpLabelsTimeout, err = time.ParseDuration(t)
			if err != nil {
				l.Fatal("Failed to parse gcp labels timeout: %v", err)
			}
		}

		// Create the API client for registering
		client := agent.NewAPIClient(l, loadAPIClientConfig(cfg, `Token`))

		// Create a template for agent registration
		agentTpl := agent.CreateAgentTemplate(l, agent.AgentTemplateConfig{
			Name:                    cfg.Name,
			Priority:                cfg.Priority,
			Tags:                    cfg.Tags,
			TagsFromEC2:             cfg.TagsFromEC2,
			TagsFromEC2Tags:         cfg.TagsFromEC2Tags,
			TagsFromGCP:             cfg.TagsFromGCP,
			TagsFromGCPLabels:       cfg.TagsFromGCPLabels,
			TagsFromHost:            cfg.TagsFromHost,
			WaitForEC2TagsTimeout:   ec2TagTimeout,
			WaitForGCPLabelsTimeout: gcpLabelsTimeout,
			ScriptEvalEnabled:       !cfg.NoCommandEval,
		})

		// Create a registrator for registering agents
		reg := agent.NewRegistrator(l, client, agentTpl)

		ag, err := reg.Register()
		if err != nil {
			l.Fatal("Failed to register agent: %v", err)
		}

		json, err := json.MarshalIndent(ag, "  ", "")
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(string(json))
	},
}
