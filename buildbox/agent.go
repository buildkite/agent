package buildbox

import "log"

type Agent struct {
  // The client the agent will use to communicate to
  // the API
  Client Client
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
