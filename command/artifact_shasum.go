package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

func ArtifactShasumCommandAction(c *cli.Context) {
	// Init debugging
	if c.Bool("debug") {
		logger.SetLevel(logger.DEBUG)
	}

	// Toggle colors
	if c.Bool("no-color") {
		logger.SetColors(false)
	}

	// Make sure we have an agent access token
	agentAccessToken := c.String("agent-access-token")
	if agentAccessToken == "" {
		logger.Fatal("An agent access token is required")
	}

	// Validate that an artifact search query was provided
	if len(c.Args()) != 1 {
		logger.Fatal("No artifact search query was provided")
	}

	// Find the build id
	buildId := c.String("build")
	if buildId == "" {
		logger.Fatal("No build was provided")
	}

	// Get the search query
	searchQuery := c.Args()[0]
	jobQuery := c.String("job")

	// Set the agent options
	var agent buildkite.Agent

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("endpoint")

	if jobQuery == "" {
		logger.Info("Searching for artifacts \"%s\"", searchQuery)
	} else {
		logger.Info("Searching for artifacts \"%s\" within job \"%s\"", searchQuery, jobQuery)
	}

	// Search for artifacts (only those that have finished uploaded) to download
	artifacts, err := agent.Client.SearchArtifacts(buildId, searchQuery, jobQuery, "finished")
	if err != nil {
		logger.Fatal("Failed to find artifacts: %s", err)
	}

	artifactsFoundLength := len(artifacts)

	if artifactsFoundLength == 0 {
		logger.Fatal("No artifacts found for downloading")
	} else if artifactsFoundLength > 1 {
		logger.Fatal("Multiple artifacts were found. Try being more specific or scope by job")
	} else {
		logger.Debug("Artifact \"%s\" found", artifacts[0].Path)

		fmt.Printf("%s\n", artifacts[0].Sha1Sum)
	}
}
