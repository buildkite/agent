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

  // Whether to run the agent in Debug mode
  Debug bool

  // Stop the agent when all work is complete
  ExitOnComplete bool

  // The boostrap script to run
  BootstrapScript string
}

func (a Agent) Run() {
  // Tell the user that debug mode has been enabled
  if a.Debug {
    log.Printf("Debug mode enabled")
  }

  // Should the client also run in Debug mode?
  a.Client.Debug = a.Debug

  // TODO
  // Get agent information from API
  a.Name = "hello123"

  // A nice welcome message
  log.Printf("Starting up buildbox-agent `%s` (version %s)...\n", a.Name, Version)

  if a.ExitOnComplete {
    a.work()
  } else {
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
