package agent

import (
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

// LocalJobLogsConfig records explicit operator choices before the agent
// removes configuration environment variables. Explicit false is an opt-out.
type LocalJobLogsConfig struct {
	EnabledSet         bool
	OTLPDestinationSet bool
}

// ApplyControlPlaneJobLogs applies registration policy to one worker without
// overriding explicit local enablement or mixing exporter destinations.
func ApplyControlPlaneJobLogs(l logger.Logger, conf *AgentConfiguration, logs *api.AgentJobLogs, local LocalJobLogsConfig) {
	if logs == nil || !logs.Enabled {
		return
	}
	if local.EnabledSet && !conf.JobLogsOTLP {
		l.Infof("Ignoring control-plane job logs: explicitly disabled locally")
		return
	}
	conf.JobLogsOTLP = true
	if local.OTLPDestinationSet {
		l.Infof("Control-plane job logs enabled; exporter ignored (local OTLP destination takes precedence)")
		return
	}
	if logs.Exporter != nil && logs.Exporter.Endpoint != "" {
		conf.ControlPlaneJobLogsExporter = logs.Exporter
		l.Infof("Control-plane job logs enabled: exporter=%s", sanitizedEndpoint(logs.Exporter.Endpoint))
	}
}
