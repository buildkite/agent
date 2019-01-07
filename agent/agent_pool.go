package agent

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/metrics"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/agent/signalwatcher"
	"github.com/buildkite/agent/system"
	"github.com/denisbrodbeck/machineid"
)

type AgentPool struct {
	Logger                *logger.Logger
	APIClient             *api.Client
	Token                 string
	ConfigFilePath        string
	Name                  string
	Priority              string
	Tags                  []string
	TagsFromEC2           bool
	TagsFromEC2Tags       bool
	TagsFromGCP           bool
	TagsFromHost          bool
	WaitForEC2TagsTimeout time.Duration
	Endpoint              string
	Debug                 bool
	DisableHTTP2          bool
	DisableColors         bool
	AgentConfiguration    *AgentConfiguration
	MetricsCollector      *metrics.Collector
	Spawn                 int

	interruptCount int
	signalLock     sync.Mutex
}

func (r *AgentPool) Start() error {
	// Show the welcome banner and config options used
	r.ShowBanner()

	// Create the agent registration API Client
	r.APIClient = APIClient{
		Endpoint:     r.Endpoint,
		Token:        r.Token,
		DisableHTTP2: r.DisableHTTP2,
		Logger:       r.Logger,
	}.Create()

	var wg sync.WaitGroup
	var errs = make(chan error, r.Spawn)

	for i := 0; i < r.Spawn; i++ {
		if r.Spawn == 1 {
			r.Logger.Info("Registering agent with Buildkite...")
		} else {
			r.Logger.Info("Registering agent %d of %d with Buildkite...", i+1, r.Spawn)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.startWorker(); err != nil {
				errs<-err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	r.Logger.Info("Started %d Agent(s)", r.Spawn)
	r.Logger.Info("You can press Ctrl-C to stop the agents")

	return <-errs
}

func (r *AgentPool) startWorker() error {
	registered, err := r.RegisterAgent(r.CreateAgentTemplate())
	if err != nil {
		return err
	}

	r.Logger.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
		strings.Join(registered.Tags, ", "))

	// Create a prefixed logger for some context in concurrent output
	l := r.Logger.WithPrefix(registered.Name)

	l.Debug("Ping interval: %ds", registered.PingInterval)
	l.Debug("Job status interval: %ds", registered.JobStatusInterval)
	l.Debug("Heartbeat interval: %ds", registered.HearbeatInterval)

	// Now that we have a registered agent, we can connect it to the API,
	// and start running jobs.
	worker := AgentWorker{
		Logger:         l,
		Agent:              registered,
		AgentConfiguration: r.AgentConfiguration,
		Endpoint:           r.Endpoint,
		Debug:              r.Debug,
		DisableHTTP2:       r.DisableHTTP2,
		MetricsCollector:   r.MetricsCollector,
	}.Create()

	l.Info("Connecting to Buildkite...")
	if err := worker.Connect(); err != nil {
		return err
	}

	if r.AgentConfiguration.DisconnectAfterJob {
		l.Info("Waiting for job to be assigned...")
		l.Info("The agent will automatically disconnect after %d seconds if no job is assigned", r.AgentConfiguration.DisconnectAfterJobTimeout)
	} else {
		l.Info("Waiting for work...")
	}

	// Start a signalwatcher so we can monitor signals and handle shutdowns
	signalwatcher.Watch(func(sig signalwatcher.Signal) {
		r.signalLock.Lock()
		defer r.signalLock.Unlock()

		if sig == signalwatcher.QUIT {
			l.Debug("Received signal `%s`", sig.String())
			worker.Stop(false)
		} else if sig == signalwatcher.TERM || sig == signalwatcher.INT {
			l.Debug("Received signal `%s`", sig.String())
			if r.interruptCount == 0 {
				r.interruptCount++
				l.Info("Received CTRL-C, send again to forcefully kill the agent")
				worker.Stop(true)
			} else {
				l.Info("Forcefully stopping running jobs and stopping the agent")
				worker.Stop(false)
			}
		} else {
			l.Debug("Ignoring signal `%s`", sig.String())
		}
	})

	// Starts the agent worker.
	if err := worker.Start(); err != nil {
		return err
	}

	// Now that the agent has stopped, we can disconnect it
	l.Info("Disconnecting %s...", worker.Agent.Name)
	if err := worker.Disconnect(); err != nil {
		return err
	}

	return nil
}

// Takes the options passed to the CLI, and creates an api.Agent record that
// will be sent to the Buildkite Agent API for registration.
func (r *AgentPool) CreateAgentTemplate() *api.Agent {
	agent := &api.Agent{
		Name:              r.Name,
		Priority:          r.Priority,
		Tags:              r.Tags,
		ScriptEvalEnabled: r.AgentConfiguration.CommandEval,
		Version:           Version(),
		Build:             BuildVersion(),
		PID:               os.Getpid(),
		Arch:              runtime.GOARCH,
	}

	// get a unique identifier for the underlying host
	if machineID, err := machineid.ProtectedID("buildkite-agent"); err != nil {
		r.Logger.Warn("Failed to find unique machine-id: %v", err)
	} else {
		agent.MachineID = machineID
	}

	// Attempt to add the EC2 tags
	if r.TagsFromEC2 {
		r.Logger.Info("Fetching EC2 meta-data...")

		err := retry.Do(func(s *retry.Stats) error {
			tags, err := EC2MetaData{}.Get()
			if err != nil {
				r.Logger.Warn("%s (%s)", err, s)
			} else {
				r.Logger.Info("Successfully fetched EC2 meta-data")
				for tag, value := range tags {
					agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", tag, value))
				}
				s.Break()
			}

			return err
		}, &retry.Config{Maximum: 5, Interval: 1 * time.Second, Jitter: true})

		// Don't blow up if we can't find them, just show a nasty error.
		if err != nil {
			r.Logger.Error(fmt.Sprintf("Failed to fetch EC2 meta-data: %s", err.Error()))
		}
	}

	// Attempt to add the EC2 tags
	if r.TagsFromEC2Tags {
		r.Logger.Info("Fetching EC2 tags...")
		err := retry.Do(func(s *retry.Stats) error {
			tags, err := EC2Tags{}.Get()
			// EC2 tags are apparently "eventually consistent" and sometimes take several seconds
			// to be applied to instances. This error will cause retries.
			if err == nil && len(tags) == 0 {
				err = errors.New("EC2 tags are empty")
			}
			if err != nil {
				r.Logger.Warn("%s (%s)", err, s)
			} else {
				r.Logger.Info("Successfully fetched EC2 tags")
				for tag, value := range tags {
					agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", tag, value))
				}
				s.Break()
			}
			return err
		}, &retry.Config{Maximum: 5, Interval: r.WaitForEC2TagsTimeout / 5, Jitter: true})

		// Don't blow up if we can't find them, just show a nasty error.
		if err != nil {
			r.Logger.Error(fmt.Sprintf("Failed to find EC2 tags: %s", err.Error()))
		}
	}

	// Attempt to add the Google Cloud meta-data
	if r.TagsFromGCP {
		tags, err := GCPMetaData{}.Get()
		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			r.Logger.Error(fmt.Sprintf("Failed to fetch Google Cloud meta-data: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	var err error

	// Add the hostname
	agent.Hostname, err = os.Hostname()
	if err != nil {
		r.Logger.Warn("Failed to find hostname: %s", err)
	}

	// Add the OS dump
	agent.OS, err = system.VersionDump(r.Logger)
	if err != nil {
		r.Logger.Warn("Failed to find OS information: %s", err)
	}

	// Attempt to add the host tags
	if r.TagsFromHost {
		agent.Tags = append(agent.Tags,
			fmt.Sprintf("hostname=%s", agent.Hostname),
			fmt.Sprintf("os=%s", runtime.GOOS),
		)
		if agent.MachineID != "" {
			agent.Tags = append(agent.Tags, fmt.Sprintf("machine-id=%s", agent.MachineID))
		}
	}

	return agent
}

// Takes the agent template and returns a registered agent. The registered
// agent includes the Access Token used to communicate with the Buildkite Agent
// API
func (r *AgentPool) RegisterAgent(agent *api.Agent) (*api.Agent, error) {
	var registered *api.Agent
	var err error
	var resp *api.Response

	register := func(s *retry.Stats) error {
		registered, resp, err = r.APIClient.Agents.Register(agent)
		if err != nil {
			if resp != nil && resp.StatusCode == 401 {
				r.Logger.Warn("Buildkite rejected the registration (%s)", err)
				s.Break()
			} else {
				r.Logger.Warn("%s (%s)", err, s)
			}
		}

		return err
	}

	// Try to register, retrying every 10 seconds for a maximum of 30 attempts (5 minutes)
	err = retry.Do(register, &retry.Config{Maximum: 30, Interval: 10 * time.Second})

	return registered, err
}

// Shows the welcome banner and the configuration options used when starting
// this agent.
func (r *AgentPool) ShowBanner() {
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

	if !r.DisableColors {
		fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
	} else {
		fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
	}

	r.Logger.Notice("Starting buildkite-agent v%s with PID: %s", Version(), fmt.Sprintf("%d", os.Getpid()))
	r.Logger.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	r.Logger.Notice("For questions and support, email us at: hello@buildkite.com")

	if r.ConfigFilePath != "" {
		r.Logger.Info("Configuration loaded from: %s", r.ConfigFilePath)
	}

	r.Logger.Debug("Bootstrap command: %s", r.AgentConfiguration.BootstrapScript)
	r.Logger.Debug("Build path: %s", r.AgentConfiguration.BuildPath)
	r.Logger.Debug("Hooks directory: %s", r.AgentConfiguration.HooksPath)
	r.Logger.Debug("Plugins directory: %s", r.AgentConfiguration.PluginsPath)

	if !r.AgentConfiguration.SSHKeyscan {
		r.Logger.Info("Automatic ssh-keyscan has been disabled")
	}

	if !r.AgentConfiguration.CommandEval {
		r.Logger.Info("Evaluating console commands has been disabled")
	}

	if !r.AgentConfiguration.PluginsEnabled {
		r.Logger.Info("Plugins have been disabled")
	}

	if !r.AgentConfiguration.RunInPty {
		r.Logger.Info("Running builds within a pseudoterminal (PTY) has been disabled")
	}

	if r.AgentConfiguration.DisconnectAfterJob {
		r.Logger.Info("Agent will disconnect after a job run has completed with a timeout of %d seconds", r.AgentConfiguration.DisconnectAfterJobTimeout)
	}
}
