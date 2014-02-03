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

func (a Agent) Prepare() {
  // TODO
  // Get agent information from API
  a.Name = "hello123"

  // A nice welcome message
  log.Printf("Starting up buildbox-agent-(%s) (%s)...\n", Version, a.Name)
}

func (a Agent) Start() {
  // How long the agent will wait when no jobs can be found.
  idleSeconds := 5
  sleepTime := time.Duration(idleSeconds * 1000) * time.Millisecond

  for {
    // The agent will run all the jobs in the queue, and return
    // when there's nothing left to do.
    a.Work()

    // Sleep then check again later.
    time.Sleep(sleepTime)
  }
}

func (a Agent) Work() {
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
