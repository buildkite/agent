package agent

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/system"
	"github.com/buildkite/roko"
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
func Register(ctx context.Context, l logger.Logger, ac APIClient, req api.AgentRegisterRequest) (*api.AgentRegisterResponse, error) {

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

	var registered *api.AgentRegisterResponse
	var resp *api.Response

	register := func(r *roko.Retrier) error {
		reg, rsp, err := ac.Register(ctx, &req)
		if err != nil {
			if resp != nil && resp.StatusCode == 401 {
				l.Warn("Buildkite rejected the registration (%s)", err)
				r.Break()
			} else {
				l.Warn("%s (%s)", err, r)
			}
			return err
		}
		registered, resp = reg, rsp
		return nil
	}

	// Try to register, retrying every 10 seconds for a maximum of 30 attempts (5 minutes)
	err := roko.NewRetrier(
		roko.WithMaxAttempts(30),
		roko.WithStrategy(roko.Constant(10*time.Second)),
	).DoWithContext(ctx, register)
	if err != nil {
		return registered, err
	}

	l.Info("Successfully registered agent \"%s\" with tags [%s]", registered.Name,
		strings.Join(registered.Tags, ", "))

	l.Debug("Ping interval: %ds", registered.PingInterval)
	l.Debug("Job status interval: %ds", registered.JobStatusInterval)
	l.Debug("Heartbeat interval: %ds", registered.HeartbeatInterval)

	if registered.Endpoint != "" {
		l.Debug("Endpoint: %s", registered.Endpoint)
	}

	return registered, nil
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
