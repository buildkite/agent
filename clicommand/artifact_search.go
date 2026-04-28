package clicommand

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/artifact"
	"github.com/urfave/cli"
)

const searchHelpDescription = `Usage:

    buildkite-agent artifact search [options] <query>

Description:

Searches for build artifacts specified by <query> on Buildkite

Note: You need to ensure that your search query is surrounded by quotes if
using a wild card as the built-in shell path globbing will provide files,
which will break the search.

Example:

    $ buildkite-agent artifact search "pkg/*.tar.gz" --build xxx

This will search across all uploaded artifacts in a build for files that match that query.
The first argument is the search query.

If you're trying to find a specific file, and there are multiple artifacts from different
jobs, you can target the particular job you want to search the artifacts from using --step:

    $ buildkite-agent artifact search "pkg/*.tar.gz" --step "tests" --build xxx

You can also use the step's job id (provided by the environment variable $BUILDKITE_JOB_ID)

Output formatting can be altered with the --format flag as follows:

    $ buildkite-agent artifact search "*" --format "%p\n"

The above will return a list of filenames separated by newline.`

const artifactSearchHelpTemplate = `{{.Description}}

Options:

{{range .VisibleFlags}}  {{.}}
{{end}}
Format specifiers:

  %i    UUID of the artifact

  %p    Artifact path

  %c    Artifact creation time (an ISO 8601 / RFC-3339 formatted UTC timestamp)

  %j    UUID of the job that uploaded the artifact, helpful for subsequent artifact downloads

  %s    File size of the artifact in bytes

  %S    SHA1 checksum of the artifact

  %T    SHA256 checksum of the artifact

  %u    Download URL for the artifact, though consider using 'buildkite-agent artifact download' instead`

type ArtifactSearchConfig struct {
	GlobalConfig
	APIConfig

	Query              string `cli:"arg:0" label:"artifact search query" validate:"required"`
	Step               string `cli:"step"`
	Build              string `cli:"build" validate:"required"`
	IncludeRetriedJobs bool   `cli:"include-retried-jobs"`
	AllowEmptyResults  bool   `cli:"allow-empty-results"`
	PrintFormat        string `cli:"format"`
}

var ArtifactSearchCommand = cli.Command{
	Name:               "search",
	Usage:              "Searches artifacts in Buildkite",
	Description:        searchHelpDescription,
	CustomHelpTemplate: artifactSearchHelpTemplate,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:  "step",
			Value: "",
			Usage: "Scope the search to a particular step by using either its name or job ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			EnvVar: "BUILDKITE_BUILD_ID",
			Usage:  "The build that the artifacts were uploaded to",
		},
		cli.BoolFlag{
			Name:   "include-retried-jobs",
			EnvVar: "BUILDKITE_AGENT_INCLUDE_RETRIED_JOBS",
			Usage:  "Include artifacts from retried jobs in the search (default: false)",
		},
		cli.BoolFlag{
			Name:  "allow-empty-results",
			Usage: "By default, searches exit 1 if there are no results. If this flag is set, searches will exit 0 with an empty set (default: false)",
		},
		cli.StringFlag{
			Name:  "format",
			Value: "%j %p %c\\n", // Note: users supply \n in the flag value literally
			Usage: "Output formatting of results. See below for listing of available format specifiers.",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[ArtifactSearchConfig](ctx, c)
		defer done()

		printFormat := cfg.PrintFormat
		if strings.Contains(printFormat, `"`) {
			// Otherwise this would break the strconv.Unquote
			printFormat = strings.ReplaceAll(printFormat, `"`, `\"`)
		}
		// Handling all escape sequences, like \n, \t etc
		unquoted, err := strconv.Unquote(`"` + printFormat + `"`)
		if err != nil {
			return fmt.Errorf("unable to parse format %q", printFormat)
		}
		printFormat = unquoted

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Setup the searcher and try get the artifacts
		searcher := artifact.NewSearcher(l, client, cfg.Build)
		artifacts, err := searcher.Search(ctx, cfg.Query, cfg.Step, cfg.IncludeRetriedJobs, true)
		if err != nil {
			return err
		}

		if len(artifacts) == 0 {
			if !cfg.AllowEmptyResults {
				return fmt.Errorf("no matches found for %q", cfg.Query)
			}
			l.Info("No matches found for %q", cfg.Query)
		}

		for _, artifact := range artifacts {
			r := strings.NewReplacer(
				"%p", artifact.Path,
				"%c", artifact.CreatedAt.Format(time.RFC3339),
				"%j", artifact.JobID,
				"%s", strconv.FormatInt(artifact.FileSize, 10),
				"%S", artifact.Sha1Sum,
				"%T", artifact.Sha256Sum,
				"%u", artifact.URL,
				"%i", artifact.ID,
			)
			if _, err := fmt.Fprint(c.App.Writer, r.Replace(printFormat)); err != nil {
				return err
			}
		}

		return nil
	},
}
