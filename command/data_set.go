package command

import (
	"fmt"
	"github.com/buildbox/agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
)

func DataSetCommandAction(c *cli.Context) {
	// Init debugging
	if c.Bool("debug") {
		buildbox.LoggerInitDebug()
	}

	agentAccessToken := c.String("agent-access-token")
	if agentAccessToken == "" {
		fmt.Println("buildbox-agent: missing agent access token\nSee 'buildbox data get --help'")
		os.Exit(1)
	}

	jobId := c.String("job")
	if jobId == "" {
		fmt.Printf("buildbox-agent: missing job\nSee 'buildbox data get --help'\n")
		os.Exit(1)
	}

	// Create a client so we can register the agent
	var client buildbox.Client
	client.AuthorizationToken = agentAccessToken
	client.URL = c.String("url")

	// Find the job
	job, err := client.JobFind(jobId)
	if err != nil {
		buildbox.Logger.Fatalf("Could not find job: %s", jobId)
	}

	// Grab the key and value to set
	key := c.Args()[0]
	value := c.Args()[1]

	// Set the data through the API
	_, err = client.DataSet(job, key, value)
	if err != nil {
		buildbox.Logger.Fatalf("Failed to set data: %s", err)
	}
}
