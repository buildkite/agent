package clicommand

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

var UploadHelpDescription = `Usage:

   buildkite-agent artifact upload [arguments...] <files...>

Description:

   Uploads files to a job as artifacts.

Example:

   $ buildkite-agent artifact upload log/test.log tmp/capybara/screenshots/**/*

   You can also upload directly to Amazon S3 if you'd like to host your own artifacts:

   $ export BUILDKITE_S3_ACCESS_KEY_ID=xxx
   $ export BUILDKITE_S3_SECRET_ACCESS_KEY=yyy
   $ export BUILDKITE_S3_DEFAULT_REGION=eu-central-1 # default is us-east-1
   $ export BUILDKITE_S3_ACL=private # default is public-read
   $ buildkite-agent artifact upload --destination s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID log/**/*.log

   Or upload directly to Google Cloud Storage:

   $ export BUILDKITE_GS_ACL=private
   $ buildkite-agent artifact upload --destination gs://name-of-your-gs-bucket/$BUILDKITE_JOB_ID log/**/*.log

	 Use an environment variable to change all artifact uploads:

	 $ export BUILDKITE_ARTIFACT_UPLOAD_DESTINATION="s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID"
	 $ buildkite-agent artifact upload log/**/*.log`

type ArtifactUploadConfig struct {
	UploadPaths      []string `cli:"arg:*" label:"upload paths" validate:"required"`
	Destination      string   `cli:"destination" label:"destination"`
	Job              string   `cli:"job" validate:"required"`
	AgentAccessToken string   `cli:"agent-access-token" validate:"required"`
	Endpoint         string   `cli:"endpoint" validate:"required"`
	NoColor          bool     `cli:"no-color"`
	Debug            bool     `cli:"debug"`
	DebugHTTP        bool     `cli:"debug-http"`
}

var deprecatedDestinationRegexp = regexp.MustCompile(`^(?:s3|gs)://`)
var deprecatedGlobRegexp = regexp.MustCompile(`\*|\?|\[.+\]|` + regexp.QuoteMeta(string(os.PathListSeparator)))

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
			Name: "destination",
			Value: "",
			Usage: "Upload to a custom destination",
			EnvVar: "BUILDKITE_ARTIFACT_UPLOAD_DESTINATION",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := ArtifactUploadConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Handle some deprecations

		// If the last upload path starts with "s3://" or "gs://" then it is an
		// upload destination, so pop it off the upload paths, set the destination,
		// and warn of deprecation
		lastUploadPath := cfg.UploadPaths[len(cfg.UploadPaths) - 1]
		if deprecatedDestinationRegexp.MatchString(lastUploadPath) {
			cfg.Destination = lastUploadPath
			cfg.UploadPaths = cfg.UploadPaths[0:len(cfg.UploadPaths) - 1]
			logger.Warn("DEPRECATED: Upload destination should now be supplied as an option:\nbuildkite-agent artifact upload --destination %q %s", cfg.Destination, strings.Trim(fmt.Sprintf("%q", cfg.UploadPaths), "[]"))
		}

		if len(cfg.UploadPaths) == 1 && deprecatedDestinationRegexp.MatchString(cfg.UploadPaths[0]) {
			suggestedArgs := strings.Split(cfg.UploadPaths[0], ";")
			suggestedArgsString := strings.Trim(fmt.Sprintf("%q", cfg.UploadPaths), "[]")
			logger.Warn("DEPRECATED: Upload paths should now be supplied as arguments:\nbuildkite-agent artifact upload %s", suggestedArgsString)
			paths = []string{}
			cfg.UploadPaths = paths
		}

		// Setup the uploader
		uploader := agent.ArtifactUploader{
			APIClient: agent.APIClient{
				Endpoint: cfg.Endpoint,
				Token:    cfg.AgentAccessToken,
			}.Create(),
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
