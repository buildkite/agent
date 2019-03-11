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

type AgentPoolConfig struct {
	ConfigFilePath          string
	Name                    string
	Priority                string
	Tags                    []string
	TagsFromEC2             bool
	TagsFromEC2Tags         bool
	TagsFromGCP             bool
	TagsFromGCPLabels       bool
	TagsFromHost            bool
	WaitForEC2TagsTimeout   time.Duration
	WaitForGCPLabelsTimeout time.Duration
	Debug                   bool
	DisableColors           bool
	Spawn                   int
	AgentConfiguration      *AgentConfiguration
	APIClientConfig         APIClientConfig
}

type AgentPool struct {
	conf             AgentPoolConfig
	logger           *logger.Logger
	apiClient        *api.Client
	metricsCollector *metrics.Collector
	interruptCount   int
	signalLock       sync.Mutex
}

func NewAgentPool(l *logger.Logger, m *metrics.Collector, c AgentPoolConfig) *AgentPool {
	return &AgentPool{
		conf:             c,
		logger:           l,
		metricsCollector: m,
	}
}

func (r *AgentPool) RegisterOnly() error {
	r.logger.Info("Registering agent with Buildkite...")

	registered, err := r.registerAgent(r.createAgentTemplate())
	if err != nil {
		return err
	}

	r.logger.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
		strings.Join(registered.Tags, ", "))

	// Create a prefixed logger for some context in concurrent output
	l := r.logger.WithPrefix(registered.Name)

	l.Debug("Ping interval: %ds", registered.PingInterval)
	l.Debug("Job status interval: %ds", registered.JobStatusInterval)
	l.Debug("Heartbeat interval: %ds", registered.HeartbeatInterval)
	l.Info("Agent Access Token: %q", registered.AccessToken)

	return nil
}

func interpolateAgentName(agentName string) string {
	hostname, err := os.Hostname()
	if err == nil {
		agentName = strings.Replace(agentName, "%hostname", hostname, -1)
	}
	agentName = strings.Replace(agentName, "%n", "1", -1)
	return agentName
}

func (r *AgentPool) StartWithoutRegister(agentName, accessToken string) error {
	// Create an agent registration with placeholders
	agent := &api.Agent{
		AccessToken:       accessToken,
		Name:              interpolateAgentName(agentName),
		PingInterval:      1,
		JobStatusInterval: 5,
		HeartbeatInterval: 5,
	}

	// Show the welcome banner and config options used
	r.showBanner()

	worker := NewAgentWorker(r.logger, agent, r.metricsCollector, AgentWorkerConfig{
		Endpoint:           r.conf.APIClientConfig.Endpoint,
		DisableHTTP2:       r.conf.APIClientConfig.DisableHTTP2,
		Debug:              r.conf.Debug,
		AgentConfiguration: r.conf.AgentConfiguration,
	})

	r.logger.Debug("Skipping agent registration as an access token was provided")
	r.logger.Info("Connecting to Buildkite...")
	if err := worker.Connect(); err != nil {
		return err
	}

	r.showWaitingForWork(r.logger)

	// Listen for shutdown and interrupt signals
	r.handleSignals(r.logger, worker)

	// Starts the agent worker.
	if err := worker.Start(); err != nil {
		return err
	}

	// Now that the agent has stopped, we can disconnect it
	r.logger.Info("Disconnecting...")
	if err := worker.Disconnect(); err != nil {
		return err
	}

	return nil
}

func (r *AgentPool) Start() error {
	// Show the welcome banner and config options used
	r.showBanner()

	var wg sync.WaitGroup
	var errs = make(chan error, r.conf.Spawn)

	for i := 0; i < r.conf.Spawn; i++ {
		if r.conf.Spawn == 1 {
			r.logger.Info("Registering agent with Buildkite...")
		} else {
			r.logger.Info("Registering agent %d of %d with Buildkite...", i+1, r.conf.Spawn)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.spawnWorker(); err != nil {
				errs <- err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	r.logger.Info("Started %d Agent(s)", r.conf.Spawn)
	r.logger.Info("You can press Ctrl-C to stop the agents")

	return <-errs
}

func (r *AgentPool) spawnWorker() error {
	registered, err := r.registerAgent(r.createAgentTemplate())
	if err != nil {
		return err
	}

	r.logger.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
		strings.Join(registered.Tags, ", "))

	// Create a prefixed logger for some context in concurrent output
	l := r.logger.WithPrefix(registered.Name)

	l.Debug("Ping interval: %ds", registered.PingInterval)
	l.Debug("Job status interval: %ds", registered.JobStatusInterval)
	l.Debug("Heartbeat interval: %ds", registered.HeartbeatInterval)

	// Now that we have a registered agent, we can connect it to the API,
	// and start running jobs.
	worker := NewAgentWorker(l, registered, r.metricsCollector, AgentWorkerConfig{
		AgentConfiguration: r.conf.AgentConfiguration,
		Debug:              r.conf.Debug,
		Endpoint:           r.conf.APIClientConfig.Endpoint,
		DisableHTTP2:       r.conf.APIClientConfig.DisableHTTP2,
	})

	l.Info("Connecting to Buildkite...")
	if err := worker.Connect(); err != nil {
		return err
	}

	r.showWaitingForWork(l)

	// Listen for shutdown and interrupt signals
	r.handleSignals(l, worker)

	// Starts the agent worker.
	if err := worker.Start(); err != nil {
		return err
	}

	// Now that the agent has stopped, we can disconnect it
	l.Info("Disconnecting %s...", registered.Name)
	if err := worker.Disconnect(); err != nil {
		return err
	}

	return nil
}

func (r *AgentPool) handleSignals(l *logger.Logger, worker *AgentWorker) {
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
}

// Takes the options passed to the CLI, and creates an api.Agent record that
// will be sent to the Buildkite Agent API for registration.
func (r *AgentPool) createAgentTemplate() *api.Agent {
	agent := &api.Agent{
		Name:              r.conf.Name,
		Priority:          r.conf.Priority,
		Tags:              r.conf.Tags,
		ScriptEvalEnabled: r.conf.AgentConfiguration.CommandEval,
		Version:           Version(),
		Build:             BuildVersion(),
		PID:               os.Getpid(),
		Arch:              runtime.GOARCH,
	}

	// get a unique identifier for the underlying host
	if machineID, err := machineid.ProtectedID("buildkite-agent"); err != nil {
		r.logger.Warn("Failed to find unique machine-id: %v", err)
	} else {
		agent.MachineID = machineID
	}

	// Attempt to add the EC2 tags
	if r.conf.TagsFromEC2 {
		r.logger.Info("Fetching EC2 meta-data...")

		err := retry.Do(func(s *retry.Stats) error {
			tags, err := EC2MetaData{}.Get()
			if err != nil {
				r.logger.Warn("%s (%s)", err, s)
			} else {
				r.logger.Info("Successfully fetched EC2 meta-data")
				for tag, value := range tags {
					agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", tag, value))
				}
				s.Break()
			}

			return err
		}, &retry.Config{Maximum: 5, Interval: 1 * time.Second, Jitter: true})

		// Don't blow up if we can't find them, just show a nasty error.
		if err != nil {
			r.logger.Error(fmt.Sprintf("Failed to fetch EC2 meta-data: %s", err.Error()))
		}
	}

	// Attempt to add the EC2 tags
	if r.conf.TagsFromEC2Tags {
		r.logger.Info("Fetching EC2 tags...")
		err := retry.Do(func(s *retry.Stats) error {
			tags, err := EC2Tags{}.Get()
			// EC2 tags are apparently "eventually consistent" and sometimes take several seconds
			// to be applied to instances. This error will cause retries.
			if err == nil && len(tags) == 0 {
				err = errors.New("EC2 tags are empty")
			}
			if err != nil {
				r.logger.Warn("%s (%s)", err, s)
			} else {
				r.logger.Info("Successfully fetched EC2 tags")
				for tag, value := range tags {
					agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", tag, value))
				}
				s.Break()
			}
			return err
		}, &retry.Config{Maximum: 5, Interval: r.conf.WaitForEC2TagsTimeout / 5, Jitter: true})

		// Don't blow up if we can't find them, just show a nasty error.
		if err != nil {
			r.logger.Error(fmt.Sprintf("Failed to find EC2 tags: %s", err.Error()))
		}
	}

	// Attempt to add the Google Cloud meta-data
	if r.conf.TagsFromGCP {
		tags, err := GCPMetaData{}.Get()
		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			r.logger.Error(fmt.Sprintf("Failed to fetch Google Cloud meta-data: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Attempt to add the Google Compute instance labels
	if r.conf.TagsFromGCPLabels {
		r.logger.Info("Fetching GCP instance labels...")
		err := retry.Do(func(s *retry.Stats) error {
			labels, err := GCPLabels{}.Get()
			if err == nil && len(labels) == 0 {
				err = errors.New("GCP instance labels are empty")
			}
			if err != nil {
				r.logger.Warn("%s (%s)", err, s)
			} else {
				r.logger.Info("Successfully fetched GCP instance labels")
				for label, value := range labels {
					agent.Tags = append(agent.Tags, fmt.Sprintf("%s=%s", label, value))
				}
				s.Break()
			}
			return err
		}, &retry.Config{Maximum: 5, Interval: r.conf.WaitForGCPLabelsTimeout / 5, Jitter: true})

		// Don't blow up if we can't find them, just show a nasty error.
		if err != nil {
			r.logger.Error(fmt.Sprintf("Failed to find GCP instance labels: %s", err.Error()))
		}
	}

	var err error

	// Add the hostname
	agent.Hostname, err = os.Hostname()
	if err != nil {
		r.logger.Warn("Failed to find hostname: %s", err)
	}

	// Add the OS dump
	agent.OS, err = system.VersionDump(r.logger)
	if err != nil {
		r.logger.Warn("Failed to find OS information: %s", err)
	}

	// Attempt to add the host tags
	if r.conf.TagsFromHost {
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
func (r *AgentPool) registerAgent(agent *api.Agent) (*api.Agent, error) {
	apiClient := NewAPIClient(r.logger, APIClientConfig{
		Endpoint:     r.conf.APIClientConfig.Endpoint,
		DisableHTTP2: r.conf.APIClientConfig.DisableHTTP2,
		Token:        r.conf.APIClientConfig.Token,
	})

	var registered *api.Agent
	var err error
	var resp *api.Response

	register := func(s *retry.Stats) error {
		registered, resp, err = apiClient.Agents.Register(agent)
		if err != nil {
			if resp != nil && resp.StatusCode == 401 {
				r.logger.Warn("Buildkite rejected the registration (%s)", err)
				s.Break()
			} else {
				r.logger.Warn("%s (%s)", err, s)
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
func (r *AgentPool) showBanner() {
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

	if !r.conf.DisableColors {
		fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
	} else {
		fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
	}

	r.logger.Notice("Starting buildkite-agent v%s with PID: %s", Version(), fmt.Sprintf("%d", os.Getpid()))
	r.logger.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	r.logger.Notice("For questions and support, email us at: hello@buildkite.com")

	if r.conf.ConfigFilePath != "" {
		r.logger.Info("Configuration loaded from: %s", r.conf.ConfigFilePath)
	}

	r.logger.Debug("Bootstrap command: %s", r.conf.AgentConfiguration.BootstrapScript)
	r.logger.Debug("Build path: %s", r.conf.AgentConfiguration.BuildPath)
	r.logger.Debug("Hooks directory: %s", r.conf.AgentConfiguration.HooksPath)
	r.logger.Debug("Plugins directory: %s", r.conf.AgentConfiguration.PluginsPath)

	if !r.conf.AgentConfiguration.SSHKeyscan {
		r.logger.Info("Automatic ssh-keyscan has been disabled")
	}

	if !r.conf.AgentConfiguration.CommandEval {
		r.logger.Info("Evaluating console commands has been disabled")
	}

	if !r.conf.AgentConfiguration.PluginsEnabled {
		r.logger.Info("Plugins have been disabled")
	}

	if !r.conf.AgentConfiguration.RunInPty {
		r.logger.Info("Running builds within a pseudoterminal (PTY) has been disabled")
	}

	if r.conf.AgentConfiguration.DisconnectAfterJob {
		r.logger.Info("Agent will disconnect after a job run has completed with a timeout of %d seconds", r.conf.AgentConfiguration.DisconnectAfterJobTimeout)
	}
}

func (r *AgentPool) showWaitingForWork(l *logger.Logger) {
	if r.conf.AgentConfiguration.DisconnectAfterJob {
		l.Info("Waiting for job to be assigned...")
		l.Info("The agent will automatically disconnect after %d seconds if no job is assigned", r.conf.AgentConfiguration.DisconnectAfterJobTimeout)
	} else if r.conf.AgentConfiguration.DisconnectAfterIdleTimeout > 0 {
		l.Info("Waiting for job to be assigned...")
		l.Info("The agent will automatically disconnect after %d seconds of inactivity", r.conf.AgentConfiguration.DisconnectAfterIdleTimeout)
	} else {
		l.Info("Waiting for work...")
	}
}
