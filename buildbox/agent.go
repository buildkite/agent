package buildbox

import (
  "log"
  "fmt"
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

func (a Agent) String() string {
  return fmt.Sprintf("Agent{Name: %s}", a.Name)
}

func (c *Client) AgentUpdate(agent *Agent) error {
  return c.Put(&agent, "/", agent)
}

func (a Agent) Run() {
  // Tell the user that debug mode has been enabled
  if a.Debug {
    log.Printf("Debug mode enabled")
  }

  // Should the client also run in Debug mode?
  a.Client.Debug = a.Debug

  // Get agent information from API. It will populate the
  // current agent struct with data.
  err := a.Client.AgentUpdate(&a)
  if err != nil {
    log.Fatal(err)
  }

  // A nice welcome message
  log.Printf("Started buildbox-agent `%s` (version %s)\n", a.Name, Version)

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
      log.Fatalf("Failed to get job (%s)", err)
    }

    // If there's no ID, then there's no job.
    if job.ID == "" {
      break
    }

    job.Run(&a)
  }
}
