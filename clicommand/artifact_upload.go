package clicommand

import (
	"context"
	"fmt"
	"slices"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/artifact"
	"github.com/urfave/cli"
)

const uploadHelpDescription = `Usage:

    buildkite-agent artifact upload [options] <pattern> [destination]

Description:

Uploads files to a job as artifacts.

You need to ensure that the paths are surrounded by quotes otherwise the
built-in shell path globbing will provide the files, which is currently not
supported.

You can specify an alternate destination on Amazon S3, Google Cloud Storage
or Artifactory as per the examples below. This may be specified in the
'destination' argument, or in the 'BUILDKITE_ARTIFACT_UPLOAD_DESTINATION'
environment variable.  Otherwise, artifacts are uploaded to a
Buildkite-managed Amazon S3 bucket, where they’re retained for six months.

Example:

    $ buildkite-agent artifact upload "log/**/*.log"

You can also upload directly to Amazon S3 if you'd like to host your own artifacts:

    $ export BUILDKITE_S3_ACCESS_KEY_ID=xxx
    $ export BUILDKITE_S3_SECRET_ACCESS_KEY=yyy
    $ export BUILDKITE_S3_DEFAULT_REGION=eu-central-1 # default is us-east-1
    $ export BUILDKITE_S3_ACL=private # default is public-read
    $ buildkite-agent artifact upload "log/**/*.log" s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID

You can use Amazon IAM assumed roles by specifying the session token:

    $ export BUILDKITE_S3_SESSION_TOKEN=zzz

Or upload directly to Google Cloud Storage:

    $ export BUILDKITE_GS_ACL=private
    $ buildkite-agent artifact upload "log/**/*.log" gs://name-of-your-gs-bucket/$BUILDKITE_JOB_ID

Or upload directly to Artifactory:

    $ export BUILDKITE_ARTIFACTORY_URL=http://my-artifactory-instance.com/artifactory
    $ export BUILDKITE_ARTIFACTORY_USER=carol-danvers
    $ export BUILDKITE_ARTIFACTORY_PASSWORD=xxx
    $ buildkite-agent artifact upload "log/**/*.log" rt://name-of-your-artifactory-repo/$BUILDKITE_JOB_ID

By default, symlinks to directories will not be explored when resolving the glob, but symlinks to
files will be uploaded as the linked files. To ignore symlinks to files use:

    $ buildkite-agent artifact upload --upload-skip-symlinks "log/**/*.log"

Note: uploading symlinks to files without following them is not supported.
If you need to preserve them in a directory, we recommend creating a tar archive:

    $ tar -cvf log.tar log/**/*
    $ buildkite-agent upload log.tar`

type ArtifactUploadConfig struct {
	GlobalConfig
	APIConfig

	UploadPaths string `cli:"arg:0" label:"upload paths" validate:"required"`
	Destination string `cli:"arg:1" label:"destination" env:"BUILDKITE_ARTIFACT_UPLOAD_DESTINATION"`
	Job         string `cli:"job" validate:"required"`
	ContentType string `cli:"content-type"`

	// Uploader flags
	Literal                   bool   `cli:"literal"`
	Delimiter                 string `cli:"delimiter"`
	GlobResolveFollowSymlinks bool   `cli:"glob-resolve-follow-symlinks"`
	UploadSkipSymlinks        bool   `cli:"upload-skip-symlinks"`
	NoMultipartUpload         bool   `cli:"no-multipart-artifact-upload"`

	// deprecated
	FollowSymlinks bool `cli:"follow-symlinks" deprecated-and-renamed-to:"GlobResolveFollowSymlinks"`
}

var ArtifactUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads files to a job as artifacts",
	Description: uploadHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the artifacts be uploaded to",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "content-type",
			Value:  "",
			Usage:  "A specific Content-Type to set for the artifacts (otherwise detected)",
			EnvVar: "BUILDKITE_ARTIFACT_CONTENT_TYPE",
		},
		cli.BoolFlag{
			Name:   "literal",
			Usage:  "Disables parsing of the upload paths as glob patterns; each path will be treated as a single literal file path (default: false)",
			EnvVar: "BUILDKITE_AGENT_ARTIFACT_LITERAL",
		},
		cli.StringFlag{
			Name:   "delimiter",
			Usage:  "Changes the delimiter used to split the upload paths into multiple paths; it can be more than 1 character. When set to the empty string, no splitting occurs",
			EnvVar: "BUILDKITE_AGENT_ARTIFACT_DELIMITER",
			Value:  ";",
		},
		cli.BoolFlag{
			Name:   "glob-resolve-follow-symlinks",
			Usage:  "Follow symbolic links to directories while resolving globs. Note: this will not prevent symlinks to files from being uploaded. Use --upload-skip-symlinks to do that (default: false)",
			EnvVar: "BUILDKITE_AGENT_ARTIFACT_GLOB_RESOLVE_FOLLOW_SYMLINKS",
		},
		cli.BoolFlag{
			Name:   "upload-skip-symlinks",
			Usage:  "After the glob has been resolved to a list of files to upload, skip uploading those that are symlinks to files (default: false)",
			EnvVar: "BUILDKITE_ARTIFACT_UPLOAD_SKIP_SYMLINKS",
		},
		cli.BoolFlag{ // Deprecated
			Name:   "follow-symlinks",
			Usage:  "Follow symbolic links while resolving globs. Note this argument is deprecated. Use `--glob-resolve-follow-symlinks` instead (default: false)",
			EnvVar: "BUILDKITE_AGENT_ARTIFACT_SYMLINKS",
		},
		NoMultipartArtifactUploadFlag,
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[ArtifactUploadConfig](ctx, c)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Setup the uploader
		uploader := artifact.NewUploader(l, client, artifact.UploaderConfig{
			JobID:          cfg.Job,
			Paths:          cfg.UploadPaths,
			Destination:    cfg.Destination,
			ContentType:    cfg.ContentType,
			DebugHTTP:      cfg.DebugHTTP,
			TraceHTTP:      cfg.TraceHTTP,
			DisableHTTP2:   cfg.NoHTTP2,
			AllowMultipart: !cfg.NoMultipartUpload,
			Literal:        cfg.Literal,
			Delimiter:      cfg.Delimiter,

			// If the deprecated flag was set to true, pretend its replacement was set to true too
			// this works as long as the user only sets one of the two flags
			GlobResolveFollowSymlinks: (cfg.GlobResolveFollowSymlinks || cfg.FollowSymlinks),
			UploadSkipSymlinks:        cfg.UploadSkipSymlinks,
		})

		// Upload the artifacts
		if err := uploader.Upload(ctx); err != nil {
			return fmt.Errorf("failed to upload artifacts: %w", err)
		}

		return nil
	},
}
