package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
	"os"
	"path/filepath"
)

func ArtifactDownloadCommandAction(c *cli.Context) {
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
		fmt.Printf("buildkite-agent: missing agent access token\nSee 'buildkite-agent artifact download --help'\n")
		os.Exit(1)
	}

	if len(c.Args()) != 2 {
		fmt.Printf("buildkite-agent: invalid usage\nSee 'buildkite-agent artifact download --help'\n")
		os.Exit(1)
	}

	// Find the build id
	buildId := c.String("build")
	if buildId == "" {
		fmt.Printf("buildkite-agent: missing build\nSee 'buildkite-agent artifact download --help'\n")
		os.Exit(1)
	}

	// Get our search query and download destination
	searchQuery := c.Args()[0]
	downloadDestination := c.Args()[1]
	jobQuery := c.String("job")

	// Turn the download destination into an absolute path and confirm it exists
	downloadDestination, _ = filepath.Abs(downloadDestination)
	fileInfo, err := os.Stat(downloadDestination)
	if err != nil {
		logger.Fatal("Could not find information about destination: %s", downloadDestination)
	}
	if !fileInfo.IsDir() {
		logger.Fatal("%s is not a directory", downloadDestination)
	}

	// Set the agent options
	var agent buildkite.Agent

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("endpoint")

	// Setup the agent
	agent.Setup()

	if jobQuery == "" {
		logger.Info("Searching for artifacts: \"%s\"", searchQuery)
	} else {
		logger.Info("Searching for artifacts: \"%s\" within job: \"%s\"", searchQuery, jobQuery)
	}

	// Search for artifacts (only those that have finished uploaded) to download
	artifacts, err := agent.Client.SearchArtifacts(buildId, searchQuery, jobQuery, "finished")
	if err != nil {
		logger.Fatal("Failed to find artifacts: %s", err)
	}

	if len(artifacts) == 0 {
		logger.Info("No artifacts found for downloading")
	} else {
		logger.Info("Found %d artifacts. Starting to download to: %s", len(artifacts), downloadDestination)

		err := buildkite.DownloadArtifacts(artifacts, downloadDestination)
		if err != nil {
			logger.Fatal("Failed to download artifacts: %s", err)
		}
	}
}
