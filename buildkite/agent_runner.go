package buildkite

import (
	"fmt"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/signalwatcher"
	"os"
	"runtime"
	"time"
)

type AgentRunner struct {
	API                              API
	ConfigFilePath                   string
	Name                             string
	Priority                         string
	BootstrapScript                  string
	BuildPath                        string
	HooksPath                        string
	MetaData                         []string
	MetaDataEC2Tags                  bool
	NoAutoSSHFingerprintVerification bool
	NoCommandEval                    bool
	NoPTY                            bool
	Endpoint                         string
	jobRunner                        *JobRunner
	stopping                         bool
}

func (a *AgentRunner) Stop(agent *Agent) {
	// Disconnect from Buildkite
	logger.Info("Disconnecting...")
	agent.Disconnect()

	// Kill the process
	os.Exit(0)
}

func (a *AgentRunner) Ping(agent *Agent) {
	// Perform the ping
	ping := Ping{Agent: agent}
	err := ping.Perform()
	if err != nil {
		logger.Warn("Failed to ping (%s)", err)
		return
	}

	// Is there a message that should be shown in the logs?
	if ping.Message != "" {
		logger.Info(ping.Message)
	}

	// Should the agent disconnect?
	if ping.Action == "disconnect" {
		a.Stop(agent)
		return
	}

	// Do nothing if there's no job
	if ping.Job == nil {
		return
	}

	logger.Info("Assigned job %s. Accepting...", ping.Job.ID)

	job := ping.Job
	job.API = agent.API

	// Accept the job
	err = job.Accept()
	if err != nil {
		logger.Error("Failed to accept the job (%s)", err)
		return
	}

	// Confirm that it's been accepted
	if job.State != "accepted" {
		logger.Error("Can not accept job with state `%s`", job.State)
		return
	}

	jobRunner := JobRunner{Job: job, Agent: agent}
	a.jobRunner = &jobRunner

	err = a.jobRunner.Run()
	if err != nil {
		logger.Error("Failed to run job: %s", err)
	}

	a.jobRunner = nil
}

func (a *AgentRunner) Run() error {
	welcomeMessage :=
		"\n" +
			"%s  _           _ _     _ _    _ _                                _\n" +
			" | |         (_) |   | | |  (_) |                              | |\n" +
			" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
			" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
			" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
			" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
			"                                                __/ |\n" +
			" http://buildkite.com/agent                    |___/\n%s\n"

	// Don't do colors on the banner if they aren't enabled in the logger
	if logger.ColorsEnabled() {
		fmt.Fprintf(logger.OutputPipe(), welcomeMessage, "\x1b[32m", "\x1b[0m")
	} else {
		fmt.Fprintf(logger.OutputPipe(), welcomeMessage, "", "")
	}

	logger.Notice("Starting buildkite-agent v%s with PID: %s", Version(), fmt.Sprintf("%d", os.Getpid()))
	logger.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	logger.Notice("For questions and support, email us at: hello@buildkite.com")

	// then it's been loaded and we should show which one we loaded.
	if a.ConfigFilePath != "" {
		logger.Info("Configuration loaded from: %s", a.ConfigFilePath)
	}

	var agent Agent
	var err error

	agent.BootstrapScript = a.BootstrapScript
	logger.Debug("Bootstrap script: %s", agent.BootstrapScript)

	agent.BuildPath = a.BuildPath
	logger.Debug("Build path: %s", agent.BuildPath)

	agent.HooksPath = a.HooksPath
	logger.Debug("Hooks directory: %s", agent.HooksPath)

	// Set the agents meta data
	agent.MetaData = a.MetaData

	// Should we try and grab the ec2 tags as well?
	if a.MetaDataEC2Tags {
		tags, err := EC2Tags{}.Get()

		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			logger.Error(fmt.Sprintf("Failed to find EC2 Tags: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agent.MetaData = append(agent.MetaData, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// More CLI options
	agent.Name = a.Name
	agent.Priority = a.Priority

	// Set auto fingerprint option
	agent.AutoSSHFingerprintVerification = !a.NoAutoSSHFingerprintVerification
	if !agent.AutoSSHFingerprintVerification {
		logger.Debug("Automatic SSH fingerprint verification has been disabled")
	}

	// Set script eval option
	agent.CommandEval = !a.NoCommandEval
	if !agent.CommandEval {
		logger.Debug("Evaluating console commands has been disabled")
	}

	agent.Hostname, err = os.Hostname()
	if err != nil {
		logger.Fatal("Could not retrieve hostname: %s", err)
	}

	agent.OS, _ = OSDump()
	agent.Version = Version()
	agent.PID = os.Getpid()

	// Toggle PTY
	if runtime.GOOS == "windows" {
		agent.RunInPty = false
	} else {
		agent.RunInPty = !a.NoPTY

		if !agent.RunInPty {
			logger.Debug("Running builds within a pseudoterminal (PTY) has been disabled")
		}
	}

	logger.Info("Registering agent with Buildkite...")

	// Use this API for the agent
	agent.API = a.API

	// Register the agent
	if err := agent.Register(); err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Successfully registered agent \"%s\" with meta-data %s", agent.Name, agent.MetaData)

	// Now we can switch to the Agents API access token
	agent.API.Token = agent.AccessToken

	// Start the signal watcher
	signalwatcher.Watch(func(sig signalwatcher.Signal) {
		if sig == signalwatcher.QUIT {
			logger.Debug("Received signal `%s`", sig.String())

			// If this is the second quit signal, or if the
			// agent doesnt' have a job.
			if a.stopping || a.jobRunner == nil {
				a.Stop(&agent)
			}

			if a.jobRunner != nil {
				logger.Warn("Waiting for job to finish before stopping. Send the signal again to exit immediately.")
				a.jobRunner.Kill()
			}

			a.stopping = true
		} else {
			logger.Debug("Ignoring signal `%s`", sig.String())
		}
	})

	// Connect the agent
	logger.Info("Connecting to Buildkite...")
	err = agent.Connect()
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Agent successfully connected")
	logger.Info("You can press Ctrl-C to stop the agent")
	logger.Info("Waiting for work...")

	// How long the agent will wait when no jobs can be found.
	idleSeconds := 5
	sleepTime := time.Duration(idleSeconds*1000) * time.Millisecond

	for {
		// Did the agent try and stop during the last job run?
		if a.stopping {
			a.Stop(&agent)
		} else {
			a.Ping(&agent)
		}

		// Sleep for a while before we check again
		time.Sleep(sleepTime)
	}

	return nil
}
