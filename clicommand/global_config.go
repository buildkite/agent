package clicommand

import "github.com/urfave/cli"

type GlobalConfig struct {
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var globalFlags = []cli.Flag{
	// Global flags
	NoColorFlag,
	DebugFlag,
	LogLevelFlag,
	ExperimentsFlag,
	ProfileFlag,
}

type APIConfig struct {
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var apiFlags = []cli.Flag{
	AgentAccessTokenFlag,
	EndpointFlag,
	NoHTTP2Flag,
	DebugHTTPFlag,
}

type DeprecatedConfig struct {
	NoSSHFingerprintVerification bool     `cli:"no-automatic-ssh-fingerprint-verification" deprecated-and-renamed-to:"NoSSHKeyscan"`
	MetaData                     []string `cli:"meta-data" deprecated-and-renamed-to:"Tags"`
	MetaDataEC2                  bool     `cli:"meta-data-ec2" deprecated-and-renamed-to:"TagsFromEC2"`
	MetaDataEC2Tags              bool     `cli:"meta-data-ec2-tags" deprecated-and-renamed-to:"TagsFromEC2Tags"`
	MetaDataGCP                  bool     `cli:"meta-data-gcp" deprecated-and-renamed-to:"TagsFromGCP"`
	TagsFromEC2                  bool     `cli:"tags-from-ec2" deprecated-and-renamed-to:"TagsFromEC2MetaData"`
	TagsFromGCP                  bool     `cli:"tags-from-gcp" deprecated-and-renamed-to:"TagsFromGCPMetaData"`
	DisconnectAfterJobTimeout    int      `cli:"disconnect-after-job-timeout" deprecated:"Use disconnect-after-idle-timeout instead"`
}

// Deprecated flags which will be removed in v4
var deprecatedFlags = []cli.Flag{
	cli.StringSliceFlag{
		Name:   "meta-data",
		Value:  &cli.StringSlice{},
		Hidden: true,
		EnvVar: "BUILDKITE_AGENT_META_DATA",
	},
	cli.BoolFlag{
		Name:   "meta-data-ec2",
		Hidden: true,
		EnvVar: "BUILDKITE_AGENT_META_DATA_EC2",
	},
	cli.BoolFlag{
		Name:   "meta-data-ec2-tags",
		Hidden: true,
		EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS",
	},
	cli.BoolFlag{
		Name:   "meta-data-gcp",
		Hidden: true,
		EnvVar: "BUILDKITE_AGENT_META_DATA_GCP",
	},
	cli.BoolFlag{
		Name:   "no-automatic-ssh-fingerprint-verification",
		Hidden: true,
		EnvVar: "BUILDKITE_NO_AUTOMATIC_SSH_FINGERPRINT_VERIFICATION",
	},
	cli.BoolFlag{
		Name:   "tags-from-ec2",
		Usage:  "Include the host's EC2 meta-data as tags (instance-id, instance-type, and ami-id)",
		EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2",
	},
	cli.BoolFlag{
		Name:   "tags-from-gcp",
		Usage:  "Include the host's Google Cloud instance meta-data as tags (instance-id, machine-type, preemptible, project-id, region, and zone)",
		EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP",
	},
	cli.IntFlag{
		Name:   "disconnect-after-job-timeout",
		Hidden: true,
		Usage:  "When --disconnect-after-job is specified, the number of seconds to wait for a job before shutting down",
		EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_JOB_TIMEOUT",
	},
}

func flatten(flagSets ...[]cli.Flag) []cli.Flag {
	length := 0
	for _, flagSet := range flagSets {
		length += len(flagSet)
	}

	flat := make([]cli.Flag, 0, length)
	for _, flagSet := range flagSets {
		flat = append(flat, flagSet...)
	}

	return flat
}
