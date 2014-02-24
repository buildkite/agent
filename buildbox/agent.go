package buildbox

import (
  "log"
  "fmt"
  "time"
  "strings"
  "os/exec"
)

type Agent struct {
  // The name of the agent
  Name string

  // The client the agent will use to communicate to
  // the API
  Client Client

  // The hostname of the agent
  Hostname string `json:"hostname,omitempty"`

  // Whether to run the agent in Debug mode
  Debug bool

  // The boostrap script to run
  BootstrapScript string
}

func (a Agent) String() string {
  return fmt.Sprintf("Agent{Name: %s, Hostname: %s}", a.Name, a.Hostname)
}

func (c *Client) AgentUpdate(agent *Agent) error {
  return c.Put(&agent, "/", agent)
}

func (a Agent) Setup() {
  // Tell the user that debug mode has been enabled
  if a.Debug {
    log.Printf("Debug mode enabled")
  }

  // Figure out the hostname of the current machine
  hostname, err := exec.Command("hostname").Output()
  if err != nil {
    log.Fatal(err)
  }

  // Set the hostname
  a.Hostname = strings.Trim(fmt.Sprintf("%s", hostname), "\n")

  // Get agent information from API. It will populate the
  // current agent struct with data.
  err = a.Client.AgentUpdate(&a)
  if err != nil {
    log.Fatal(err)
  }

  // A nice welcome message
  log.Printf("Started buildbox-agent `%s` (version %s)\n", a.Name, Version)
}

func (a Agent) Start() {
  // How long the agent will wait when no jobs can be found.
  idleSeconds := 5
  sleepTime := time.Duration(idleSeconds * 1000) * time.Millisecond


  log.Println(a.Debug)
  log.Println(a.Client.Debug)

  for {
    // The agent will run all the jobs in the queue, and return
    // when there's nothing left to do.
    for {
      job, err := a.Client.JobNext()
      if err != nil {
        log.Printf("Failed to get job (%s)", err)
        break
      }

      // If there's no ID, then there's no job.
      if job.ID == "" {
        break
      }

      job.Run(&a)
    }

    // Sleep then check again later.
    time.Sleep(sleepTime)
  }
}

func (a Agent) Run(id string) {
  // Try and find the job
  job, err := a.Client.JobFindAndAssign(id)

  if err != nil {
    log.Fatal(err)
  }

  if job.State != "scheduled" {
    log.Fatalf("The agent can only run scheduled jobs. Current state is `%s`", job.State)
  }

  // Run the paticular job
  job.Run(&a)
}
