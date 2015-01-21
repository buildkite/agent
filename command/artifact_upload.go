package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/codegangsta/cli"
	"os"
)

func ArtifactUploadCommandAction(c *cli.Context) {
	// Init debugging
	if c.Bool("debug") {
		buildkite.LoggerInitDebug()
	}

	agentAccessToken := c.String("agent-access-token")
	if agentAccessToken == "" {
		fmt.Printf("buildkite-agent: missing agent access token\nSee 'buildkite-agent artifact download --help'\n")
		os.Exit(1)
	}

	jobId := c.String("job")
	if jobId == "" {
		fmt.Printf("buildkite-agent: missing job\nSee 'buildkite-agent artifact download --help'\n")
		os.Exit(1)
	}

	// Grab the first argument and use as paths to download
	paths := c.Args().First()
	if paths == "" {
		fmt.Printf("buildkite-agent: missing upload paths\nSee 'buildkite-agent artifact download --help'\n")
		os.Exit(1)
	}

	// Do we have a custom destination
	destination := ""
	if len(c.Args()) > 1 {
		destination = c.Args()[1]
	}

	// Set the agent options
	var agent buildkite.Agent

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("endpoint")

	// Setup the agent
	agent.Setup()

	// Find the actual job now
	job, err := agent.Client.JobFind(jobId)
	if err != nil {
		buildkite.Logger.Fatalf("Could not find job: %s", jobId)
	}

	// Create artifact structs for all the files we need to upload
	artifacts, err := buildkite.CollectArtifacts(job, paths)
	if err != nil {
		buildkite.Logger.Fatalf("Failed to collect artifacts: %s", err)
	}

	if len(artifacts) == 0 {
		buildkite.Logger.Infof("No files matched paths: %s", paths)
	} else {
		buildkite.Logger.Infof("Found %d files that match \"%s\"", len(artifacts), paths)

		err := buildkite.UploadArtifacts(agent.Client, job, artifacts, destination)
		if err != nil {
			buildkite.Logger.Fatalf("Failed to upload artifacts: %s", err)
		}
	}
}
