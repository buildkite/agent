package buildbox

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type Agent struct {
	// The name of the agent
	Name string

	// The client the agent will use to communicate to
	// the API
	Client Client

	// The hostname of the agent
	Hostname string `json:"hostname,omitempty"`

	// The boostrap script to run
	BootstrapScript string

	// The currently running Job
	Job *Job

	// This is true if the agent should no longer accept work
	stopping bool
}

func (c *Client) AgentUpdate(agent *Agent) error {
	return c.Put(&agent, "/", agent)
}

func (c *Client) AgentCrash(agent *Agent) error {
	return c.Post(&agent, "/crash", agent)
}

func (a *Agent) String() string {
	return fmt.Sprintf("Agent{Name: %s, Hostname: %s}", a.Name, a.Hostname)
}

func (a *Agent) Setup() {
	// Figure out the hostname of the current machine
	hostname, err := exec.Command("hostname").Output()
	if err != nil {
		Logger.Fatal(err)
	}

	// Set the hostname
	a.Hostname = strings.Trim(fmt.Sprintf("%s", hostname), "\n")

	// Get agent information from API. It will populate the
	// current agent struct with data.
	err = a.Client.AgentUpdate(a)
	if err != nil {
		Logger.Fatal(err)
	}
}

func (a *Agent) MonitorSignals() {
	// Handle signals
	signals := make(chan os.Signal, 1)

	// Monitor a heap of different signals for debugging purposes. Only
	// some of them are used.
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGINT,
		syscall.SIGUSR2)

	go func() {
		// This will block until a signal is sent
		sig := <-signals

		Logger.Debugf("Received signal `%s`", sig.String())

		// Only monitor certain signals
		if sig != syscall.SIGINT && sig != syscall.SIGUSR2 {
			Logger.Debugf("Ignoring signal `%s`", sig.String())

			// Start monitoring signals again
			a.MonitorSignals()

			return
		}

		// If the agent isn't running a job, exit right away
		if a.Job == nil {
			Logger.Info("No jobs running. Exiting...")
			os.Exit(1)
		}

		// If the agent is already trying to stop and we've got another interupt,
		// just forcefully shut it down.
		if a.stopping {
			// Kill the job
			a.Job.Kill()

			// Send an API call letting BB know that the agent had to forcefully stop
			a.Client.AgentCrash(a)

			// Die time.
			os.Exit(1)
		} else {
			Logger.Info("Exiting... Waiting for job to finish before stopping. Send signal again to exit immediately.")

			a.stopping = true
		}

		// Start monitoring signals again
		a.MonitorSignals()
	}()
}

func (a *Agent) Start() {
	// How long the agent will wait when no jobs can be found.
	idleSeconds := 5
	sleepTime := time.Duration(idleSeconds*1000) * time.Millisecond

	for {
		// The agent will run all the jobs in the queue, and return
		// when there's nothing left to do.
		for {
			job, err := a.Client.JobNext()
			if err != nil {
				Logger.Errorf("Failed to get job (%s)", err)
				break
			}

			// If there's no ID, then there's no job.
			if job.ID == "" {
				break
			}

			a.Job = job
			job.Run(a)
			a.Job = nil
		}

		// Should we be stopping?
		if a.stopping {
			break
		} else {
			// Sleep then check again later.
			time.Sleep(sleepTime)
		}
	}
}

func (a *Agent) Run(id string) {
	// Try and find the job
	job, err := a.Client.JobFindAndAssign(id)

	if err != nil {
		Logger.Fatal(err)
	}

	if job.State != "scheduled" {
		Logger.Fatalf("The agent can only run scheduled jobs. Current state is `%s`", job.State)
	}

	// Run the paticular job
	job.Run(a)
}
