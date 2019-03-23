package agent

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/agent/system"
	"github.com/denisbrodbeck/machineid"
)

// AgentTemplate represents an Agent to be registered
type AgentTemplate struct {
	Name              string
	Priority          string
	Tags              []string
	ScriptEvalEnabled bool
}

// Build an api.Agent out of the Template populated with system info
func (a AgentTemplate) Build(l *logger.Logger) *api.Agent {
	agent := &api.Agent{
		Name:              a.Name,
		Priority:          a.Priority,
		Tags:              a.Tags,
		ScriptEvalEnabled: a.ScriptEvalEnabled,
		Version:           Version(),
		Build:             BuildVersion(),
		PID:               os.Getpid(),
		Arch:              runtime.GOARCH,
	}

	// get a unique identifier for the underlying host
	machineID, err := machineid.ProtectedID("buildkite-agent")
	if err != nil {
		l.Warn("Failed to find unique machine-id: %v", err)
	} else {
		agent.MachineID = machineID
	}

	// Add the hostname
	agent.Hostname, err = os.Hostname()
	if err != nil {
		l.Warn("Failed to find hostname: %s", err)
	}

	// Add the OS dump
	agent.OS, err = system.VersionDump(l)
	if err != nil {
		l.Warn("Failed to find OS information: %s", err)
	}

	return agent
}

// Register takes an AgentTemplate and registers it with the Buildkite API
func Register(l *logger.Logger, ac *api.Client, tpl AgentTemplate) (*api.Agent, error) {
	var registered *api.Agent
	var err error
	var resp *api.Response

	// Build the template into an Agent record that is filled out in the register call
	agent := tpl.Build(l)

	l.Info("Registering agent with Buildkite...")

	register := func(s *retry.Stats) error {
		registered, resp, err = ac.Agents.Register(agent)
		if err != nil {
			if resp != nil && resp.StatusCode == 401 {
				l.Warn("Buildkite rejected the registration (%s)", err)
				s.Break()
			} else {
				l.Warn("%s (%s)", err, s)
			}
		}

		return err
	}

	// Try to register, retrying every 10 seconds for a maximum of 30 attempts (5 minutes)
	err = retry.Do(register, &retry.Config{Maximum: 30, Interval: 10 * time.Second})
	if err != nil {
		l.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
			strings.Join(registered.Tags, ", "))

		l.Debug("Ping interval: %ds", registered.PingInterval)
		l.Debug("Job status interval: %ds", registered.JobStatusInterval)
		l.Debug("Heartbeat interval: %ds", registered.HearbeatInterval)

		if registered.Endpoint != "" {
			l.Debug("Endpoint: %s", registered.Endpoint)
		}
	}

	return registered, err
}
