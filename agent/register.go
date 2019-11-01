package agent

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/retry"
	"github.com/buildkite/agent/v3/system"
	"github.com/denisbrodbeck/machineid"
)

var (
	hostname      string
	machineID     string
	osVersionDump string
	cacheOnce     sync.Once
)

// Register takes an api.Agent and registers it with the Buildkite API
// and populates the result of the register call
func Register(l logger.Logger, ac APIClient, req api.AgentRegisterRequest) (*api.AgentRegisterResponse, error) {
	var registered *api.AgentRegisterResponse
	var err error
	var resp *api.Response

	// Set up some slightly expensive system info once
	cacheOnce.Do(func() { cacheRegisterSystemInfo(l) })

	// Set some static things to set on the register request
	req.Version = Version()
	req.Build = BuildVersion()
	req.PID = os.Getpid()
	req.Arch = runtime.GOARCH
	req.MachineID = machineID
	req.Hostname = hostname
	req.OS = osVersionDump

	register := func(s *retry.Stats) error {
		registered, resp, err = ac.Register(&req)
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
	if err == nil {
		l.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
			strings.Join(registered.Tags, ", "))

		l.Debug("Ping interval: %ds", registered.PingInterval)
		l.Debug("Job status interval: %ds", registered.JobStatusInterval)
		l.Debug("Heartbeat interval: %ds", registered.HeartbeatInterval)

		if registered.Endpoint != "" {
			l.Debug("Endpoint: %s", registered.Endpoint)
		}
	}

	return registered, err
}

func cacheRegisterSystemInfo(l logger.Logger) {
	var err error

	machineID, err = machineid.ProtectedID("buildkite-agent")
	if err != nil {
		l.Warn("Failed to find unique machine-id: %v", err)
	}

	hostname, err = os.Hostname()
	if err != nil {
		l.Warn("Failed to find hostname: %s", err)
	}

	osVersionDump, err = system.VersionDump(l)
	if err != nil {
		l.Warn("Failed to find OS information: %s", err)
	}
}
