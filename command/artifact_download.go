package command

import (
	"fmt"
	"github.com/buildboxhq/buildbox-agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
	"path/filepath"
)

func ArtifactDownloadCommandAction(c *cli.Context) {
	// Init debugging
	if c.Bool("debug") {
		buildbox.LoggerInitDebug()
	}

	agentAccessToken := c.String("agent-access-token")

	// Should we look to the environment for the agent access token?
	if agentAccessToken == AgentAccessTokenDefault {
		agentAccessToken = os.Getenv(AgentAccessTokenEnv)
	}

	if agentAccessToken == "" {
		fmt.Printf("%s: missing agent access token\nSee '%s help download'\n", app.Name, app.Name)
		os.Exit(1)
	}

	if len(c.Args()) != 2 {
		fmt.Printf("%s: invalid usage\nSee '%s help download'\n", app.Name, app.Name)
		os.Exit(1)
	}

	// Find the build id
	buildId := c.String("build")
	if buildId == BuildIdDefault {
		buildId = os.Getenv(BuildIdEnv)
	}

	if buildId == "" {
		fmt.Printf("%s: missing build\nSee '%s help download'\n", app.Name, app.Name)
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
		buildbox.Logger.Fatalf("Could not find information about destination: %s", downloadDestination)
	}
	if !fileInfo.IsDir() {
		buildbox.Logger.Fatalf("%s is not a directory", downloadDestination)
	}

	// Set the agent options
	var agent buildbox.Agent

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("url")

	// Setup the agent
	agent.Setup()

	if jobQuery == "" {
		buildbox.Logger.Infof("Searching for artifacts: \"%s\"", searchQuery)
	} else {
		buildbox.Logger.Infof("Searching for artifacts: \"%s\" within job: \"%s\"", searchQuery, jobQuery)
	}

	// Search for artifacts (only those that have finished uploaded) to download
	artifacts, err := agent.Client.SearchArtifacts(buildId, searchQuery, jobQuery, "finished")
	if err != nil {
		buildbox.Logger.Fatalf("Failed to find artifacts: %s", err)
	}

	if len(artifacts) == 0 {
		buildbox.Logger.Info("No artifacts found for downloading")
	} else {
		buildbox.Logger.Infof("Found %d artifacts. Starting to download to: %s", len(artifacts), downloadDestination)

		err := buildbox.DownloadArtifacts(artifacts, downloadDestination)
		if err != nil {
			buildbox.Logger.Fatalf("Failed to download artifacts: %s", err)
		}
	}
}
