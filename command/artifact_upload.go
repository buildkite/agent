package command

import (
	"fmt"
	"github.com/buildboxhq/buildbox-agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
	"path/filepath"
)

func ArtifactUploadCommandAction(c *cli.Context) {
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
		fmt.Printf("%s: missing agent access token\nSee '%s help upload'\n", app.Name, app.Name)
		os.Exit(1)
	}

	jobId := c.String("job")

	// Should we look to the environment for the job id?
	if jobId == JobIdDefault {
		jobId = os.Getenv(JobIdEnv)
	}

	if jobId == "" {
		fmt.Printf("%s: missing job\nSee '%s help upload'\n", app.Name, app.Name)
		os.Exit(1)
	}

	// Grab the first argument and use as paths to download
	paths := c.Args().First()
	if paths == "" {
		fmt.Printf("%s: missing upload paths\nSee '%s help upload'\n", app.Name, app.Name)
		os.Exit(1)
	}

	// Do we have a custom destination
	destination := ""
	if len(c.Args()) > 1 {
		destination = c.Args()[1]
	}

	// Set the agent options
	var agent buildbox.Agent

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("url")

	// Setup the agent
	agent.Setup()

	// Find the actual job now
	job, err := agent.Client.JobFind(jobId)
	if err != nil {
		buildbox.Logger.Fatalf("Could not find job: %s", jobId)
	}

	// Create artifact structs for all the files we need to upload
	artifacts, err := buildbox.CollectArtifacts(job, paths)
	if err != nil {
		buildbox.Logger.Fatalf("Failed to collect artifacts: %s", err)
	}

	if len(artifacts) == 0 {
		buildbox.Logger.Infof("No files matched paths: %s", paths)
	} else {
		buildbox.Logger.Infof("Found %d files that match \"%s\"", len(artifacts), paths)

		err := buildbox.UploadArtifacts(agent.Client, job, artifacts, destination)
		if err != nil {
			buildbox.Logger.Fatalf("Failed to upload artifacts: %s", err)
		}
	}
}
