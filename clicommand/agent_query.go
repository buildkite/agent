package clicommand

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/agent/stdin"
	"github.com/codegangsta/cli"
	"github.com/jmespath/go-jmespath"
)

var QueryDescription = `Usage:

   buildkite-agent query [arguments...]

Description:

   TODO

Example:

   $ buildkite-agent query <<GRAPHQL
       build(uuid: $BUILDKITE_BUILD_ID) { message }
     GRAPHQL`

type AgentQueryConfig struct {
	Query            string `cli:"arg:0" label:"query"`
	Expression       string `cli:"arg:1" label:"expression"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

var AgentQueryCommand = cli.Command{
	Name:        "query",
	Usage:       "Makes a query to a subset of the Buildkite GraphQL API",
	Description: StartDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job that is currently running on the agent",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := AgentQueryConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		}.Create()

		// Is there a query on STDIN?
		if stdin.IsPipe() {
			logger.Debug("Reading query from STDIN")
			bytes, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				logger.Fatal("Failed to read from STDIN: %s", err)
			}
			// Convert STDIN to a string, and also assume that what
			// was in the query field was the expression
			cfg.Expression = cfg.Query
			cfg.Query = string(bytes)
		}

		// Wrap the query with a `query` call
		cfg.Query = fmt.Sprintf("query {\n%s}", cfg.Query)

		logger.Debug("Performing GraphQL query:\n%s", cfg.Query)

		// Perform the query
		var query *api.Query
		var err error
		var resp *api.Response
		err = retry.Do(func(s *retry.Stats) error {
			query, resp, err = client.Queries.Perform(cfg.Job, cfg.Query)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400 || resp.StatusCode == 500) {
				s.Break()
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			logger.Fatal("Failed to perform query: %s", err)
		}

		if len(query.Errors) > 0 {
			errorTypeSuffix := ""
			if query.ErrorType != "" {
				errorTypeSuffix = " (%s)"
			}
			logger.Error("Failed to perform GraphQL Query%s", errorTypeSuffix)

			for _, queryError := range query.Errors {
				logger.Error("%s", queryError.Message)
			}
			os.Exit(1)
		} else {
			data := query.Data

			if cfg.Expression != "" {
				parser := jmespath.NewParser()
				_, err := parser.Parse(cfg.Expression)
				if err != nil {
					if syntaxError, ok := err.(jmespath.SyntaxError); ok {
						logger.Fatal("%s\n%s\n", syntaxError, syntaxError.HighlightLocation())
					}
					logger.Fatal("%s", err)
				}

				data, err = jmespath.Search(cfg.Expression, data)
				if err != nil {
					logger.Fatal("Error executing expression: %s", err)
				}
			}

			toJSON, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				logger.Fatal("Error serializing result to JSON: %s", err)
			}

			jsonAsString := string(toJSON)
			if jsonAsString != "null" {
				fmt.Println(jsonAsString)
			}
		}
	},
}
