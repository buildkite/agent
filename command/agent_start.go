package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/codegangsta/cli"
	"os"
)

func AgentStartCommandAction(c *cli.Context) {
	// For display purposes, come up with what the name of the agent is.
	agentName, err := buildkite.MachineHostname()
	if c.String("name") != "" {
		agentName = c.String("name")
	}

	welcomeMessage := "                _._\n" +
		"           _.-``   ''-._\n" +
		"      _.-``             ''-._\n" +
		"  .-``                       ''-._      Buildkite Agent " + buildkite.Version() + "\n" +
		" |        _______________         |\n" +
		" |      .'  ___________  '.       |     Name: " + agentName + "\n" +
		" |        .'  _______  '.         |     PID: " + fmt.Sprintf("%d", os.Getpid()) + "\n" +
		" |          .'  ___  '.           |\n" +
		" |            .' | '.             |     https://buildkite.com/agent\n" +
		" |               |                |\n" +
		" |               |                |\n" +
		"  ``._           |            _.''\n" +
		"      `._        |         _.'\n" +
		"         `._     |      _.'\n" +
		"            ``. _|_ . ''\n\n"

	fmt.Printf(welcomeMessage)

	// Init debugging
	if c.Bool("debug") {
		buildkite.LoggerInitDebug()
	}

	// Create the agent
	if c.String("access-token") != "" {
		fmt.Println("buildkite-agent: use of --access-token is now deprecated\nSee 'buildkite-agent start --help'")
		os.Exit(1)
	}

	agentRegistrationToken := c.String("token")
	if agentRegistrationToken == "" {
		fmt.Println("buildkite-agent: missing token\nSee 'buildkite-agent start --help'")
		os.Exit(1)
	}

	// Expand the envionment variable.
	bootstrapScript := os.ExpandEnv(c.String("bootstrap-script"))

	// Make sure the boostrap script exists.
	if _, err := os.Stat(bootstrapScript); os.IsNotExist(err) {
		fmt.Printf("buildkite-agent: could not find bootstrap script %s\n", bootstrapScript)
		os.Exit(1)
	}

	buildkite.Logger.Debugf("Using bootstrap script: %s", bootstrapScript)

	// Expand the build path. We don't bother checking to see if it can be
	// written to, because it'll show up in the agent logs if it doesn't
	// work.
	buildPath := os.ExpandEnv(c.String("build-path"))

	buildkite.Logger.Debugf("Using build path: %s", buildPath)

	// Create a client so we can register the agent
	var client buildkite.Client
	client.AuthorizationToken = agentRegistrationToken
	client.URL = c.String("endpoint")

	agentMetaData := c.StringSlice("meta-data")

	// Should we try and grab the ec2 tags as well?
	if c.Bool("meta-data-ec2-tags") {
		tags, err := buildkite.EC2InstanceTags()

		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			buildkite.Logger.Error(fmt.Sprintf("Failed to find EC2 Tags: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agentMetaData = append(agentMetaData, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Register the agent
	agentAccessToken, err := client.AgentRegister(c.String("name"), c.String("priority"), agentMetaData)
	if err != nil {
		buildkite.Logger.Fatal(err)
	}

	// Set the agent options
	var agent buildkite.Agent
	agent.BootstrapScript = bootstrapScript
	agent.BuildPath = buildPath
	agent.RunInPty = !c.Bool("no-pty")
	agent.AutoSSHFingerprintVerification = !c.Bool("no-automatic-ssh-fingerprint-verification")

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = c.String("endpoint")

	// Setup the agent
	agent.Setup()

	// Setup signal monitoring
	agent.MonitorSignals()

	// Start the agent
	agent.Start()
}
