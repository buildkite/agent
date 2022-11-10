package clicommand

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

type OidcTokenConfig struct {
	Audience string `cli:"audience"`
	Job      string `cli:"job"      validate:"required"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint"           validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

const (
	oidcTokenDescription = `Usage:

   buildkite-agent oidc token [options...]

Description:
   Requests and prints an OIDC token from Buildkite that claims the Job ID and
	 the specified audience. If no audience is specified, the endpoint's default
	 audience will be claimed.

Example:
   $ buildkite-agent oidc token --audience sts.amazonaws.com

   Prints the environment passed into the process
`
	backoffSeconds = 2
	maxAttempts    = 5
)

var OidcTokenCommand = cli.Command{
	Name:        "token",
	Usage:       "Requests and prints an OIDC token from Buildkite with the specified audience,",
	Description: oidcTokenDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "audience",
			Value: "",
			Usage: "The audience that will consume the OIDC token",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Buildkite Job Id to claim in the OIDC token",
			EnvVar: "BUILDKITE_JOB_ID",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		// The configuration will be loaded into this struct
		cfg := OidcTokenConfig{}

		loader := cliconfig.Loader{CLI: c, Config: &cfg}
		warnings, err := loader.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(1)
		}

		l := CreateLogger(&cfg)

		// Now that we have a logger, log out the warnings that loading config generated
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Find the meta data value
		var token *api.OidcToken
		var resp *api.Response

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(maxAttempts),
			roko.WithStrategy(roko.Exponential(backoffSeconds*time.Second, 0)),
		).Do(func(r *roko.Retrier) error {
			var audience []string
			if len(cfg.Audience) > 0 {
				audience = []string{cfg.Audience}
			}

			token, resp, err = client.OidcToken(cfg.Job, audience...)
			if resp != nil {
				fmt.Println(resp.StatusCode, r.ShouldGiveUp())

				switch resp.StatusCode {
				// Don't bother retrying if the response was one of these statuses
				case http.StatusUnauthorized | http.StatusForbidden | http.StatusUnprocessableEntity:
					r.Break()
					return err
				}
			}

			if err != nil {
				l.Warn("%s (%s)", err, r)
			}

			return err
		}); err != nil {
			if len(cfg.Audience) > 0 {
				l.Error("Could not obtain OIDC token for audience %s", cfg.Audience)
			} else {
				l.Error("Could not obtain OIDC token for default audience")
			}
			return err
		}

		fmt.Println(token.Token)
		return nil
	},
}
