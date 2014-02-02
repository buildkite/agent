package buildbox

import (
  "log"
  "time"
)

type Agent struct {
  // The name of the agent
  Name string

  // The client the agent will use to communicate to
  // the API
  Client Client
}

func (a Agent) Start() {
  // TODO
  // Get agent information from API
  a.Name = "hello123"

  // A nice welcome message
  log.Printf("Starting up buildbox-agent-(%s) (%s)...\n", Version, a.Name)

  // How long the agent will wait when no jobs can be found.
  idleSeconds := 5
  sleepTime := time.Duration(idleSeconds * 1000) * time.Millisecond

  for {
    // The agent will run all the jobs in the queue, and return
    // when there's nothing left to do.
    a.work()

    // Sleep then check again later.
    time.Sleep(sleepTime)
  }
}

func (a Agent) work() {
  for {
    // Try and find some work to do
    job, err := a.Client.JobNext()
    if err != nil {
      log.Fatal(err)
    }

    // If there's no ID, then there's no job.
    if job.ID == "" {
      break
    }

    job.Run(&a.Client)
  }
}
