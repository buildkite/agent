package main

import (
  "time"
  "log"
  "github.com/buildboxhq/buildbox-agent/buildbox"
)

func main() {
  // Create a new Client that we'll use to interact with
  // the API
  var client buildbox.Client
  client.AgentAccessToken = "e6296371ed3dd3f24881b0866506b8c6"
  client.URL = "http://agent.buildbox.dev/v1"
  client.Debug = false

  // Create a new instance of the Agent
  var agent buildbox.Agent
  agent.Client = client

  for {
    // The agent will run all the jobs in the queue, and return
    // when there's nothing left to do.
    agent.Work()

    log.Println("Sleeping for 3 seconds")
    time.Sleep(5000 * time.Millisecond)
  }
}
