package agent

import (
	"errors"
	"fmt"
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

type RegistrarConfig struct {
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
	ScriptEvalEnabled       bool
}

type Registrar struct {
	conf      RegistrarConfig
	logger    *logger.Logger
	apiClient *api.Client
}

func NewRegistrar(l *logger.Logger, ac *api.Client, c RegistrarConfig) *Registrar {
	return &Registrar{
		conf:      c,
		logger:    l,
		apiClient: ac,
	}
}

func (r *Registrar) Register() (*api.Agent, error) {
	agent := r.createAgentTemplate()

	var registered *api.Agent
	var err error
	var resp *api.Response

	register := func(s *retry.Stats) error {
		registered, resp, err = r.apiClient.Agents.Register(agent)
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

	if err != nil {
		r.logger.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
			strings.Join(registered.Tags, ", "))

		r.logger.Debug("Ping interval: %ds", registered.PingInterval)
		r.logger.Debug("Job status interval: %ds", registered.JobStatusInterval)
		r.logger.Debug("Heartbeat interval: %ds", registered.HearbeatInterval)

		if registered.Endpoint != "" {
			r.logger.Debug("Endpoint: %s", registered.Endpoint)
		}
	}

	return registered, err
}

// Creates an api.Agent record that will be sent to the
// Buildkite Agent API for registration.
func (r *Registrar) createAgentTemplate() *api.Agent {
	agent := &api.Agent{
		Name:              r.conf.Name,
		Priority:          r.conf.Priority,
		Tags:              r.conf.Tags,
		ScriptEvalEnabled: r.conf.ScriptEvalEnabled,
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
