package clicommand

import "github.com/urfave/cli"

var BuildkiteAgentCommands = []cli.Command{
	// These commands are special. The have a different lifecycle to the others
	AgentStartCommand,
	BootstrapCommand,

	// These are in alphabetical order
	AcknowledgementsCommand,
	AnnotateCommand,
	{
		Name:  "annotation",
		Usage: "Make changes to an annotation on the currently running build",
		Subcommands: []cli.Command{
			AnnotationRemoveCommand,
		},
	},
	{
		Name:  "artifact",
		Usage: "Upload/download artifacts from Buildkite jobs",
		Subcommands: []cli.Command{
			ArtifactUploadCommand,
			ArtifactDownloadCommand,
			ArtifactSearchCommand,
			ArtifactShasumCommand,
		},
	},
	{
		Name:  "build",
		Usage: "Interact with a Buildkite build",
		Subcommands: []cli.Command{
			BuildCancelCommand,
		},
	},
	{
		Name:  "env",
		Usage: "Process environment subcommands",
		Subcommands: []cli.Command{
			EnvDumpCommand,
			EnvGetCommand,
			EnvSetCommand,
			EnvUnsetCommand,
		},
	},
	GitCredentialsHelperCommand,
	{
		Name:  "lock",
		Usage: "Process lock subcommands",
		Subcommands: []cli.Command{
			LockAcquireCommand,
			LockDoCommand,
			LockDoneCommand,
			LockGetCommand,
			LockReleaseCommand,
		},
	},
	{
		Name:  "redactor",
		Usage: "Redact sensitive information from logs",
		Subcommands: []cli.Command{
			RedactorAddCommand,
		},
	},
	{
		Name:  "meta-data",
		Usage: "Get/set data from Buildkite jobs",
		Subcommands: []cli.Command{
			MetaDataSetCommand,
			MetaDataGetCommand,
			MetaDataExistsCommand,
			MetaDataKeysCommand,
		},
	},
	{
		Name:  "oidc",
		Usage: "Interact with Buildkite OpenID Connect (OIDC)",
		Subcommands: []cli.Command{
			OIDCRequestTokenCommand,
		},
	},
	{
		Name:  "pipeline",
		Usage: "Make changes to the pipeline of the currently running build",
		Subcommands: []cli.Command{
			PipelineUploadCommand,
		},
	},
	{
		Name:  "secret",
		Usage: "Interact with Pipelines Secrets",
		Subcommands: []cli.Command{
			SecretGetCommand,
		},
	},
	{
		Name:  "step",
		Usage: "Get or update an attribute of a build step, or cancel unfinished jobs for a step",
		Subcommands: []cli.Command{
			StepGetCommand,
			StepUpdateCommand,
			StepCancelCommand,
		},
	},
	AgentStopCommand,
	{
		Name:  "tool",
		Usage: "Utility commands, intended for users and operators of the agent to run directly on their machines, and not as part of a Buildkite job",
		Subcommands: []cli.Command{
			ToolKeygenCommand,
			ToolSignCommand,
		},
	},
}
