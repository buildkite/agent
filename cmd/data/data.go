package main

import (
	"fmt"
	"github.com/buildboxhq/buildbox-agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
)

var AppHelpTemplate = `A utility to set/get data on Builds on Buildbox

Usage:

  {{.Name}} command [arguments]

The comamnds are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "buildbox-data help [command]" for more information about a command.

`

var CommandHelpTemplate = `Usage: buildbox-data {{.Name}} [command options] [arguments...]

{{.Description}}

Options:
   {{range .Flags}}{{.}}
   {{end}}
`

var SetHelpDescription = `Set random data on a build using a basic key/value store.

Example:

buildbox-data set "foo" "bar" --job [job] \
                              --agent-access-token [agent-access-token]`
var GetHelpDescription = `Get data from a builds key/value store.

Example:

buildbox-data get "foo" --job [job] \
                        --agent-access-token [agent-access-token]`

var JobIdEnv = "BUILDBOX_JOB_ID"
var JobIdDefault = "$" + JobIdEnv
var AgentAccessTokenEnv = "BUILDBOX_AGENT_ACCESS_TOKEN"
var AgentAccessTokenDefault = "$" + AgentAccessTokenEnv

func main() {
	cli.AppHelpTemplate = AppHelpTemplate
	cli.CommandHelpTemplate = CommandHelpTemplate

	app := cli.NewApp()
	app.Name = "buildbox-data"
	app.Version = buildbox.Version

	// Define the actions for our CLI
	app.Commands = []cli.Command{
		{
			Name:        "set",
			Usage:       "Set data on a build",
			Description: SetHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"job", JobIdDefault, "The source job of the data"},
				cli.StringFlag{"agent-access-token", AgentAccessTokenDefault, "The access token used to identify the agent"},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The agent API endpoint"},
				cli.BoolFlag{"debug", "Enable debug mode"},
			},
			Action: func(c *cli.Context) {
				// Create the agent from the CLI options
				agent := setupAgentFromCli(c, "set")

				// Find the job from the CLI
				job := findJobFromCli(agent, c, "set")

				// There should be 2 args, the key and the value.
				if len(c.Args()) != 2 {
					fmt.Printf("buildbox-data: missing key/value pair\nSee 'buildbox-data help set'\n")
					os.Exit(1)
				}

				// Grab the key and value to set
				key := c.Args()[0]
				value := c.Args()[1]

				// Set the data through the API
				_, err := agent.Client.DataSet(job, key, value)
				if err != nil {
					buildbox.Logger.Fatalf("Failed to set data: %s", err)
				}
			},
		},
		{
			Name:        "get",
			Usage:       "Get data from a build",
			Description: GetHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"job", JobIdDefault, "The source job of the data"},
				cli.StringFlag{"agent-access-token", AgentAccessTokenDefault, "The access token used to identify the agent"},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The agent API endpoint"},
				cli.BoolFlag{"debug", "Enable debug mode"},
			},
			Action: func(c *cli.Context) {
				// Create the agent from the CLI options
				agent := setupAgentFromCli(c, "set")

				// Find the job from the CLI
				job := findJobFromCli(agent, c, "set")

				// There should be 1 arg, the key of the data.
				if len(c.Args()) != 1 {
					fmt.Printf("buildbox-data: missing key\nSee 'buildbox-data help get'\n")
					os.Exit(1)
				}

				// Grab the key
				key := c.Args()[0]

				// Get the data through the API
				data, err := agent.Client.DataGet(job, key)
				if err != nil {
					buildbox.Logger.Fatalf("Failed to get data: %s", err)
				}

				// Output it
				fmt.Print(data.Value)
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

func setupAgentFromCli(c *cli.Context, command string) *buildbox.Agent {
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
		fmt.Println("buildbox-data: missing agent access token\nSee 'buildbox-data start --help'")
		os.Exit(1)
	}

	// Set the agent options
	var agent buildbox.Agent

	// Client specific options
	agent.Client.AgentAccessToken = agentAccessToken
	agent.Client.URL = c.String("url")

	return &agent
}

func findJobFromCli(agent *buildbox.Agent, c *cli.Context, command string) *buildbox.Job {
	jobId := c.String("job")

	// Should we look to the environment for the job id?
	if jobId == JobIdDefault {
		jobId = os.Getenv(JobIdEnv)
	}

	// If it's empty, it means we don't have one - but it's required,
	// so error.
	if jobId == "" {
		fmt.Printf("buildbox-data: missing job\nSee 'buildbox-data help %s'\n", command)
		os.Exit(1)
	}

	// Find the actual job now
	job, err := agent.Client.JobFind(jobId)
	if err != nil {
		buildbox.Logger.Fatalf("Could not find job: %s", jobId)
	}

	return job
}
