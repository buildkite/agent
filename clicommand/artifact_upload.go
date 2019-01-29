package clicommand

import (
	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/urfave/cli"
)

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
   $ buildkite-agent artifact upload "log/**/*.log" s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID

   Or upload directly to Google Cloud Storage:

   $ export BUILDKITE_GS_ACL=private
   $ buildkite-agent artifact upload "log/**/*.log" gs://name-of-your-gs-bucket/$BUILDKITE_JOB_ID`

type ArtifactUploadConfig struct {
	UploadPaths      string `cli:"arg:0" label:"upload paths" validate:"required"`
	Destination      string `cli:"arg:1" label:"destination" env:"BUILDKITE_ARTIFACT_UPLOAD_DESTINATION"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

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
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		l := logger.NewLogger()

		// The configuration will be loaded into this struct
		cfg := ArtifactUploadConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(l, cfg)

		// Create the API client
		client := agent.NewAPIClient(l, agent.APIClientConfig{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		})

		// Setup the uploader
		uploader := agent.NewArtifactUploader(l, client, agent.ArtifactUploaderConfig{
			JobID:       cfg.Job,
			Paths:       cfg.UploadPaths,
			Destination: cfg.Destination,
		})

		// Upload the artifacts
		if err := uploader.Upload(); err != nil {
			l.Fatal("Failed to upload artifacts: %s", err)
		}
	},
}
