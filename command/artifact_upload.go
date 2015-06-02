package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

type ArtifactUploadConfig struct {
	UploadPaths      string `cli:"arg:0" label:"upload paths" validate:"required"`
	Destination      string `cli:"arg:1" label:"destination"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var UploadHelpDescription = `Usage:

   buildkite-agent artifact upload <pattern> <destination> [arguments...]

Description:

   Uploads files to a job as artifacts.

   You need to ensure that the paths are surrounded by quotes otherwise the
   built-in shell path globbing will provide the files, which is currently not
   supported.

Example:

   $ buildkite-agent artifact upload "log/**/*.log"

   You can also upload directly to Amazon S3 if you'd like to host your own artifacts:

   $ export BUILDKITE_S3_ACCESS_KEY_ID=xxx
   $ export BUILDKITE_S3_SECRET_ACCESS_KEY=yyy
   $ export BUILDKITE_S3_DEFAULT_REGION=eu-central-1 # default is us-east-1
   $ export BUILDKITE_S3_ACL=private # default is public-read
   $ buildkite-agent artifact upload "log/**/*.log" s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID`

var ArtifactUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads files to a job as artifacts",
	Description: UploadHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the artifacts be uploaded to",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "agent-access-token",
			Value:  "",
			Usage:  "The access token used to identify the agent",
			EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
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
		cfg := ArtifactUploadConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		SetupGlobalConfiguration(cfg)

		// Setup the uploader
		uploader := buildkite.ArtifactUploader{
			API: buildkite.API{
				Endpoint: cfg.Endpoint,
				Token:    cfg.AgentAccessToken,
			},
			JobID:       cfg.Job,
			Paths:       cfg.UploadPaths,
			Destination: cfg.Destination,
		}

		// Upload the artifacts
		if err := uploader.Upload(); err != nil {
			logger.Fatal("Failed to upload artifacts: %s", err)
		}
	},
}
