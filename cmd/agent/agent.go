package main

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/buildboxhq/buildbox-agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
)

var AppHelpTemplate = `The agent performs builds and sends the results back to Buildbox.

Usage:

  {{.Name}} command [arguments]

The comamnds are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "buildbox-agent help [command]" for more information about a command.

`

var CommandHelpTemplate = `Usage: buildbox-agent {{.Name}} [command options] [arguments...]

{{.Description}}

Options:
   {{range .Flags}}{{.}}
   {{end}}
`

var StartHelpDescription = `When a job is ready to run it will call the "bootstrap-script"
and pass it all the environment variables required for the job to run.
This script is responsible for checking out the code, and running the
actual build script defined in the project.

The agent will run any jobs within a PTY (Pseudo terminal) if available.

Example:

buildbox-agent start --access-token [access-token] \
                     --bootstrap-script ~/.buildbox/bootstrap.sh`

var RunHelpDescription = `Manually run a scheduled job. If the job has already assigned
to another agent, it will not run.

Example:

buildbox-agent run [job-id] --access-token [access-token]`

var BootstrapScriptDefault = "$HOME/.buildbox/bootstrap.sh"

var AgentAccessTokenEnv = "BUILDBOX_AGENT_ACCESS_TOKEN"
var AgentAccessTokenDefault = "$" + AgentAccessTokenEnv

var AgentRegistrationTokenEnv = "BUILDBOX_AGENT_REGISTRATION_TOKEN"
var AgentRegistrationTokenDefault = "$" + AgentRegistrationTokenEnv

func main() {
	cli.AppHelpTemplate = AppHelpTemplate
	cli.CommandHelpTemplate = CommandHelpTemplate

	app := cli.NewApp()
	app.Name = "buildbox-agent"
	app.Version = buildbox.Version

	// Define the actions for our CLI
	app.Commands = []cli.Command{
		{
			Name:        "register",
			Usage:       "Registers an agent with Buildbox and starts it",
			Description: StartHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"agent-registration-token", AgentAccessTokenDefault, "The agent registration token for your account."},
				cli.StringFlag{"bootstrap-script", BootstrapScriptDefault, "Path to the bootstrap script."},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The Agent API endpoint."},
				cli.BoolFlag{"debug", "Enable debug mode."},
			},
			Action: func(c *cli.Context) {
				// Register an agent with Buildbox
				agent := registerAgentFromCli(c)

				// Setup signal monitoring
				agent.MonitorSignals()

				// Start the agent
				agent.Start()
			},
		},
		{
			Name:        "start",
			Usage:       "Starts the Buildbox agent",
			Description: StartHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"access-token", AgentAccessTokenDefault, "The access token used to identify the agent."},
				cli.StringFlag{"bootstrap-script", BootstrapScriptDefault, "Path to the bootstrap script."},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The Agent API endpoint."},
				cli.BoolFlag{"debug", "Enable debug mode."},
			},
			Action: func(c *cli.Context) {
				// Create the agent from the CLI options
				agent := setupAgentFromCli(c, "start")

				// Setup signal monitoring
				agent.MonitorSignals()

				// Start the agent
				agent.Start()
			},
		},
		{
			Name:        "run",
			Usage:       "Manually run a scheduled job",
			Description: RunHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"access-token", "", "The access token used to identify the agent."},
				cli.StringFlag{"bootstrap-script", BootstrapScriptDefault, "Path to the bootstrap script."},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The Agent API endpoint."},
				cli.BoolFlag{"debug", "Enable debug mode."},
			},
			Action: func(c *cli.Context) {
				// Create the agent from the CLI options
				agent := setupAgentFromCli(c, "run")

				// Grab the first argument and use as the job id
				id := c.Args().First()

				// Validate the job id
				if id == "" {
					fmt.Printf("buildbox-agent: no job id specified.\nSee 'buildbox-agent help run'")
					os.Exit(1)
				}

				agent.Run(id)
			},
		},
	}

	// Default the default action
	app.Action = func(c *cli.Context) {
		cli.ShowAppHelp(c)
		os.Exit(1)
	}

	app.Run(os.Args)
}

func registerAgentFromCli(c *cli.Context) *buildbox.Agent {
	// Init debugging
	if c.Bool("debug") {
		buildbox.LoggerInitDebug()
	}

	agentRegistrationToken := c.String("agent-registration-token")

	// Should we look to the environment for the agent registration token?
	if agentRegistrationToken == AgentRegistrationTokenDefault {
		agentRegistrationToken = os.Getenv(AgentRegistrationTokenDefault)
	}

	if agentRegistrationToken == "" {
		fmt.Println("buildbox-agent: missing agent registration token\nSee 'buildbox-agent register --help'")
		os.Exit(1)
	}

	// Create a client so we can register the agent
	var client buildbox.Client
	client.AuthorizationToken = agentRegistrationToken
	client.URL = c.String("url")

	// Register the agent
	agentAccessToken, err := client.AgentRegister()
	if err != nil {
		buildbox.Logger.Fatal(err)
	}

	// Start the agent using the token we have
	return setupAgent("register", agentAccessToken, c.String("bootstrap-script"), c.String("url"))
}

func setupAgentFromCli(c *cli.Context, command string) *buildbox.Agent {
	// Init debugging
	if c.Bool("debug") {
		buildbox.LoggerInitDebug()
	}

	return setupAgent(command, c.String("access-token"), c.String("bootstrap-script"), c.String("url"))
}

func setupAgent(command string, agentAccessToken string, bootstrapScript string, url string) *buildbox.Agent {
	// Should we look to the environment for the agent access token?
	if agentAccessToken == AgentAccessTokenDefault {
		agentAccessToken = os.Getenv(AgentAccessTokenEnv)
	}

	if agentAccessToken == "" {
		fmt.Println("buildbox-agent: missing agent access token\nSee 'buildbox-agent " + command + " --help'")
		os.Exit(1)
	}

	// Expand the envionment variable.
	if bootstrapScript == BootstrapScriptDefault {
		bootstrapScript = os.ExpandEnv(bootstrapScript)
	}

	// Make sure the boostrap script exists.
	if _, err := os.Stat(bootstrapScript); os.IsNotExist(err) {
		print("buildbox-agent: no such file " + bootstrapScript + "\n")
		os.Exit(1)
	}

	// Set the agent options
	var agent buildbox.Agent
	agent.BootstrapScript = bootstrapScript

	// Client specific options
	agent.Client.AuthorizationToken = agentAccessToken
	agent.Client.URL = url

	// Setup the agent
	agent.Setup()

	// A nice welcome message
	buildbox.Logger.WithFields(logrus.Fields{
		"pid":     os.Getpid(),
		"version": buildbox.Version,
	}).Infof("Started buildbox-agent `%s`", agent.Name)

	return &agent
}
