package command

import (
	"fmt"
	"github.com/buildbox/agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
)

func AgentStartCommandAction(c *cli.Context) {
	// For display purposes, come up with what the name of the agent is.
	agentName := buildbox.MachineHostname()
	if c.String("name") != "" {
		agentName = c.String("name")
	}

	welcomeMessage := "                _._\n" +
		"           _.-``   ''-._\n" +
		"      _.-``             ''-._\n" +
		"  .-``                       ''-._      Buildbox Agent " + buildbox.Version + "\n" +
		" |        _______________         |\n" +
		" |      .'  ___________  '.       |     Name: " + agentName + "\n" +
		" |        .'  _______  '.         |     PID: " + fmt.Sprintf("%d", os.Getpid()) + "\n" +
		" |          .'  ___  '.           |\n" +
		" |            .' | '.             |     https://buildbox.io/agent\n" +
		" |               |                |\n" +
		" |               |                |\n" +
		"  ``._           |            _.''\n" +
		"      `._        |         _.'\n" +
		"         `._     |      _.'\n" +
		"            ``. _|_ . ''\n\n"

	fmt.Printf(welcomeMessage)

	// Init debugging
	if c.Bool("debug") {
		buildbox.LoggerInitDebug()
	}

	// Create the agent
	if c.String("access-token") != "" {
		fmt.Println("buildbox-agent: use of --access-token is now deprecated\nSee 'buildbox-agent start --help'")
		os.Exit(1)
	}

	agentRegistrationToken := c.String("token")
	if agentRegistrationToken == "" {
		fmt.Println("buildbox-agent: missing token\nSee 'buildbox-agent start --help'")
		os.Exit(1)
	}

	// Expand the envionment variable.
	bootstrapScript := os.ExpandEnv(c.String("bootstrap-script"))

	// Make sure the boostrap script exists.
	if _, err := os.Stat(bootstrapScript); os.IsNotExist(err) {
		fmt.Printf("buildbox-agent: could not find bootstrap script %s\n", bootstrapScript)
		os.Exit(1)
	}

	buildbox.Logger.Debugf("Using bootstrap script: %s", bootstrapScript)

	// Create a client so we can register the agent
	var client buildbox.Client
	client.AuthorizationToken = agentRegistrationToken
	client.URL = c.String("endpoint")

	agentMetaData := c.StringSlice("meta-data")

	// Should we try and grab the ec2 tags as well?
	if c.Bool("meta-data-ec2-tags") {
		tags, err := buildbox.EC2InstanceTags()

		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			buildbox.Logger.Error(fmt.Sprintf("Failed to find EC2 Tags: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agentMetaData = append(agentMetaData, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Register the agent
	agentAccessToken, err := client.AgentRegister(c.String("name"), c.String("priority"), agentMetaData)
	if err != nil {
		buildbox.Logger.Fatal(err)
	}

	// Start the agent using the token we have
	agent := setupAgent(agentAccessToken, bootstrapScript, !c.Bool("no-pty"), c.String("endpoint"))

	// Setup signal monitoring
	agent.MonitorSignals()

	// Start the agent
	agent.Start()
}

func setupAgent(agentAccessToken string, bootstrapScript string, runInPty bool, url string) *buildbox.Agent {
	// Set the agent options
	var agent buildbox.Agent
	agent.BootstrapScript = bootstrapScript
	agent.RunInPty = runInPty

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = url

	// Setup the agent
	agent.Setup()

	return &agent
}
