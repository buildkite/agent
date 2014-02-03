package main

import (
  "github.com/buildboxhq/buildbox-agent-go/buildbox"
)

/*
func start(cli *Cli) {
  // Raise an error if no access token has been set.
  if cli.AccessToken == "" {
    fmt.Printf("Error: Missing access token. See 'buildbox-agent-%s --help'\n", Version)
    os.Exit(1)
  }

  // Tell the user that debug mode has been enabled
  if cli.Debug {
    log.Printf("Debug mode enabled")
  }

  // Create a new Client that we'll use to interact with the API
  var client buildbox.Client
  client.AgentAccessToken = cli.AccessToken
  client.URL = cli.URL
  client.Debug = cli.Debug

  // Create a new instance of the Agent
  var agent buildbox.Agent
  agent.Client = client

  // Get the agent ready for action
  agent.Prepare()

  if cli.ExitOnComplete {
    agent.Work()
  } else {
    agent.Start()
  }
}
*/

func main() {
  // Run our cli stuffs
  var cli buildbox.Cli
  cli.Run()
}
