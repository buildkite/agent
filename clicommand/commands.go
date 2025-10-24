package clicommand

import "github.com/urfave/cli"

const (
	categoryJobCommands = "Commands that can be run within a Buildkite job"
	categoryInternal    = "Internal commands, not intended to be run by users"
)

var BuildkiteAgentCommands = []cli.Command{
	// These commands are special. The have a different lifecycle to the others
	AgentStartCommand,
	BootstrapCommand,
	KubernetesBootstrapCommand,

	// These are in alphabetical order
	AcknowledgementsCommand,
	AnnotateCommand,
	{
		Name:     "annotation",
		Category: categoryJobCommands,
		Usage:    "Make changes to annotations on the currently running build",
		Subcommands: []cli.Command{
			AnnotationRemoveCommand,
		},
	},
	{
		Name:     "artifact",
		Category: categoryJobCommands,
		Usage:    "Upload/download artifacts from Buildkite jobs",
		Subcommands: []cli.Command{
			ArtifactUploadCommand,
			ArtifactDownloadCommand,
			ArtifactSearchCommand,
			ArtifactShasumCommand,
		},
	},
	{
		Name:     "build",
		Category: categoryJobCommands,
		Usage:    "Interact with a Buildkite build",
		Subcommands: []cli.Command{
			BuildCancelCommand,
		},
	},
	{
		Name:     "cache",
		Category: categoryJobCommands,
		Usage:    "Manage build caches",
		Hidden:   true, // currently in experimental phase
		Subcommands: []cli.Command{
			CacheSaveCommand,
			CacheRestoreCommand,
		},
	},
	{
		Name:     "env",
		Category: categoryJobCommands,
		Usage:    "Interact with the environment of the currently running build",
		Subcommands: []cli.Command{
			EnvDumpCommand,
			EnvGetCommand,
			EnvSetCommand,
			EnvUnsetCommand,
		},
	},
	GitCredentialsHelperCommand,
	{
		Name:     "lock",
		Category: categoryJobCommands,
		Usage:    "Lock or unlock resources for the currently running build",
		Subcommands: []cli.Command{
			LockAcquireCommand,
			LockDoCommand,
			LockDoneCommand,
			LockGetCommand,
			LockReleaseCommand,
		},
	},
	{
		Name:     "redactor",
		Category: categoryJobCommands,
		Usage:    "Redact sensitive information from logs",
		Subcommands: []cli.Command{
			RedactorAddCommand,
		},
	},
	{
		Name:     "meta-data",
		Category: categoryJobCommands,
		Usage:    "Get/set metadata from Buildkite jobs",
		Subcommands: []cli.Command{
			MetaDataSetCommand,
			MetaDataGetCommand,
			MetaDataExistsCommand,
			MetaDataKeysCommand,
		},
	},
	{
		Name:     "oidc",
		Category: categoryJobCommands,
		Usage:    "Interact with Buildkite OpenID Connect (OIDC)",
		Subcommands: []cli.Command{
			OIDCRequestTokenCommand,
		},
	},
	AgentPauseCommand,
	{
		Name:     "pipeline",
		Category: categoryJobCommands,
		Usage:    "Make changes to the pipeline of the currently running build",
		Subcommands: []cli.Command{
			PipelineUploadCommand,
		},
	},
	AgentResumeCommand,
	{
		Name:     "secret",
		Category: categoryJobCommands,
		Usage:    "Interact with Pipelines Secrets",
		Subcommands: []cli.Command{
			SecretGetCommand,
		},
	},
	{
		Name:     "step",
		Category: categoryJobCommands,
		Usage:    "Get or update an attribute of a build step, or cancel unfinished jobs for a step",
		Subcommands: []cli.Command{
			StepGetCommand,
			StepUpdateCommand,
			StepCancelCommand,
		},
	},
	AgentStopCommand,
	{
		Name:  "tool",
		Usage: "Utilities for working with the Buildkite Agent",
		Subcommands: []cli.Command{
			ToolKeygenCommand,
			ToolSignCommand,
		},
	},
}
