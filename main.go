package main

import (
  "log"
  "fmt"
  "flag"
  "os"
  "github.com/buildboxhq/buildbox-agent-go/buildbox"
)

func main() {
  // Set the flags that we'll use
  accessToken := flag.String("access-token", "", "The access token used to identify the agent.")
  debug := flag.Bool("debug", false, "Runs the agent in debug mode.")
  url := flag.String("url", "", "Specify a different API endpoint.")

  // Parse the flags
  flag.Parse()

  // Raise an error if no access token has been set.
  if *accessToken == "" {
    fmt.Printf("Error: Missing access token. See 'buildbox-agent-%s --help'\n", buildbox.Version)
    os.Exit(1)
  }

  // Tell the user that debug mode has been enabled
  if *debug {
    log.Printf("Debug mode enabled")
  }

  // Create a new Client that we'll use to interact with the API
  var client buildbox.Client
  client.AgentAccessToken = *accessToken
  client.URL = *url
  client.Debug = *debug

  // Create a new instance of the Agent
  var agent buildbox.Agent
  agent.Client = client

  // Kick the agent off, and start some jobs!
  agent.Start()
}
