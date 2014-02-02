package main

import (
  "time"
  "log"
  "fmt"
  "github.com/buildboxhq/buildbox-agent-go/buildbox"
)

func main() {
  processName := fmt.Sprintf("buildbox-agent-%s", buildbox.Version)
  log.Printf("Starting up %s...\n", processName)

  // Create a new Client that we'll use to interact with
  // the API
  var client buildbox.Client
  client.AgentAccessToken = "e6296371ed3dd3f24881b0866506b8c6"
  client.URL = "http://agent.buildbox.dev/v1"
  client.Debug = false

  // Create a new instance of the Agent
  var agent buildbox.Agent
  agent.Client = client

  idleSeconds := 5
  sleepTime := time.Duration(idleSeconds * 1000) * time.Millisecond

  for {
    // The agent will run all the jobs in the queue, and return
    // when there's nothing left to do.
    agent.Work()

    // Sleep then check again later.
    time.Sleep(sleepTime)
  }
}
