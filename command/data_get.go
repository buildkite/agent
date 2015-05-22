package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
	"os"
)

func DataGetCommandAction(c *cli.Context) {
	// Init debugging
	if c.Bool("debug") {
		logger.SetLevel(logger.DEBUG)
	}

	// Toggle colors
	if c.Bool("no-color") {
		logger.SetColors(false)
	}

	agentAccessToken := c.String("agent-access-token")
	if agentAccessToken == "" {
		fmt.Println("buildkite-agent: missing agent access token\nSee 'buildkite data get --help'")
		os.Exit(1)
	}

	jobId := c.String("job")
	if jobId == "" {
		fmt.Printf("buildkite-agent: missing job\nSee 'buildkite data get --help'\n")
		os.Exit(1)
	}

	// Create a client so we can register the agent
	var client buildkite.Client
	client.AuthorizationToken = agentAccessToken
	client.URL = c.String("endpoint")

	// Grab the key
	key := c.Args()[0]

	// Get the data through the API
	data, err := client.DataGet(jobId, key)
	if err != nil {
		logger.Fatal("Failed to get data: %s", err)
	}

	// Output it
	fmt.Print(data.Value)
}
