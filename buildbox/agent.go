package buildbox

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Agent struct {
	// The name of the agent
	Name string

	// The client the agent will use to communicate to
	// the API
	Client Client

	// The PID of the agent
	PID int `json:"pid,omitempty"`

	// The hostname of the agent
	Hostname string `json:"hostname,omitempty"`

	// The boostrap script to run
	BootstrapScript string

	// Run jobs in a PTY
	RunInPty bool

	// The currently running Job
	Job *Job

	// This is true if the agent should no longer accept work
	stopping bool
}

func (c *Client) AgentConnect(agent *Agent) error {
	return c.Post(&agent, "/connect", agent)
}

func (c *Client) AgentDisconnect(agent *Agent) error {
	return c.Post(&agent, "/disconnect", agent)
}

func (a *Agent) String() string {
	return fmt.Sprintf("Agent{Name: %s, Hostname: %s, PID: %d, RunInPty: %t}", a.Name, a.Hostname, a.PID, a.RunInPty)
}

func (a *Agent) Setup() {
	// Set the hostname
	a.Hostname = MachineHostname()

	// Set the PID of the agent
	a.PID = os.Getpid()

	// Get agent information from API. It will populate the
	// current agent struct with data.
	err := a.Client.AgentConnect(a)
	if err != nil {
		Logger.Fatal(err)
	}
}

func (a *Agent) MonitorSignals() {
	// Handle signals
	signals := make(chan os.Signal, 1)

	// Monitor a heap of different signals for debugging purposes. Only
	// some of them are used. Descriptions are copied from
	// http://en.wikipedia.org/wiki/Unix_signal so when working with this
	// code, we know exactly what each signal should do (and  alwsays
	// forget the specifics)

	signal.Notify(signals, os.Interrupt,
		// The SIGHUP signal is sent to a process when its controlling
		// terminal is closed. It was originally designed to notify the
		// process of a serial line drop (a hangup). In modern systems,
		// this signal usually means that the controlling pseudo or
		// virtual terminal has been closed.[3] Many daemons will
		// reload their configuration files and reopen their logfiles
		// instead of exiting when receiving this signal.[4] nohup is a
		// command to make a command ignore the signal.
		syscall.SIGHUP,

		// The SIGTERM signal is sent to a process to request its
		// termination. Unlike the SIGKILL signal, it can be caught and
		// interpreted or ignored by the process.  This allows the
		// process to perform nice termination releasing resources and
		// saving state if appropriate. It should be noted that SIGINT
		// is nearly identical to SIGTERM.
		syscall.SIGTERM,

		// The SIGKILL signal is sent to a process to cause it to
		// terminate immediately (kill). In contrast to SIGTERM and
		// SIGINT, this signal cannot be caught or ignored, and the
		// receiving process cannot perform any clean-up upon receiving
		// this signal.
		syscall.SIGKILL,

		// The SIGINT signal is sent to a process by its controlling
		// terminal when a user wishes to interrupt the process. This
		// is typically initiated by pressing Control-C, but on some
		// systems, the "delete" character or "break" key can be
		// used.[5]
		syscall.SIGINT,

		// The SIGQUIT signal is sent to a process by its controlling
		// terminal when the user requests that the process quit and
		// perform a core dump.
		syscall.SIGQUIT,

		// The SIGUSR1 and SIGUSR2 signals are sent to a process to
		// indicate user-defined conditions. In this case SIGUSR1 will
		// show debug information about the agnent, and SIGUSR2 will
		// restart the agent.
		syscall.SIGUSR1,
		syscall.SIGUSR2)

	go func() {
		// This will block until a signal is sent
		sig := <-signals

		Logger.Debugf("Received signal `%s`", sig.String())

		// Ignore SIGHUP, some terminals seem to send it when they get resized,
		// killing the process here would just be silly.
		if sig == syscall.SIGHUP {
			Logger.Debugf("Ignoring signal `%s`", sig.String())

			// Start monitoring signals again
			a.MonitorSignals()

			return
		}

		// If we've received a SIGKILL, die immediately.
		if sig == syscall.SIGKILL {
			Logger.Debugf("Exiting immediately", sig.String())

			os.Exit(1)
		}

		// Show debug information with SIGUSR1
		if sig == syscall.SIGUSR1 {
			Logger.Debugf("======DEBUG===== %s", a)
			if a.Job != nil {
				Logger.Debugf("======DEBUG===== %s", a.Job)
			}

			// Start monitoring signals again
			a.MonitorSignals()

			return
		}

		// If the agent isn't running a job, exit right away
		if a.Job == nil {
			// Disconnect from Buildbox
			Logger.Info("Disconnecting...")
			a.Client.AgentDisconnect(a)

			os.Exit(1)
		}

		// If the agent is already trying to stop and we've got another interupt,
		// just forcefully shut it down.
		if a.stopping {
			// Kill the job
			a.Job.Kill()

			// Disconnect from Buildbox
			Logger.Info("Disconnecting...")
			a.Client.AgentDisconnect(a)

			// Die time.
			os.Exit(1)
		} else {
			Logger.Info("Exiting... Waiting for job to finish before stopping. Send signal again to exit immediately")

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

			Logger.Infof("Assigned job %s. Accepting...", job.ID)

			// Accept the job
			job, err = a.Client.JobAccept(job)
			if err != nil {
				Logger.Errorf("Failed to accept the job (%s)", err)
				break
			}

			// Confirm that it's been accepted
			if job.State != "accepted" {
				Logger.Errorf("Can not accept job with state `%s`", job.State)
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
