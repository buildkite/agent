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

type OIDCTokenConfig struct {
	Audience string `cli:"audience"`
	Lifetime int    `cli:"lifetime"`
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

   buildkite-agent oidc request-token [options...]

Description:
   Requests and prints an OIDC token from Buildkite that claims the Job ID
   (amongst other things) and the specified audience. If no audience is
   specified, the endpoint's default audience will be claimed.

Example:
   $ buildkite-agent oidc request-token --audience sts.amazonaws.com

   Requests and prints an OIDC token from Buildkite that claims the Job ID
   (amongst other things) and the audience "sts.amazonaws.com".
`
	backoffSeconds = 2
	maxAttempts    = 5
)

var OIDCRequestTokenCommand = cli.Command{
	Name:        "request-token",
	Usage:       "Requests and prints an OIDC token from Buildkite with the specified audience,",
	Description: oidcTokenDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "audience",
			Usage: "The audience that will consume the OIDC token. The API will choose a default audience if it is omitted.",
		},
		cli.IntFlag{
			Name:  "lifetime",
			Usage: "The time (in seconds) the OIDC token will be valid for before expiry. Must be a non-negative integer. If the flag is omitted or set to 0, the API will choose a default finite lifetime.",
		},
		cli.StringFlag{
			Name:   "job",
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
		cfg := OIDCTokenConfig{}

		loader := cliconfig.Loader{CLI: c, Config: &cfg}
		warnings, err := loader.Load()
		if err != nil {
			fmt.Fprintf(c.App.ErrWriter, "%s\n", err)
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

		// Note: if --lifetime is omitted, cfg.Lifetime = 0
		if cfg.Lifetime < 0 {
			l.Fatal("Lifetime %d must be a non-negative integer.", cfg.Lifetime)
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Request the token
		var token *api.OIDCToken

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(maxAttempts),
			roko.WithStrategy(roko.Exponential(backoffSeconds*time.Second, 0)),
		).Do(func(r *roko.Retrier) error {
			req := &api.OIDCTokenRequest{
				Job:      cfg.Job,
				Audience: cfg.Audience,
				Lifetime: cfg.Lifetime,
			}

			var resp *api.Response
			token, resp, err = client.OIDCToken(req)
			if resp != nil {
				switch resp.StatusCode {
				// Don't bother retrying if the response was one of these statuses
				case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity:
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
