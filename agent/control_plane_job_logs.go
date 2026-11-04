package agent

import (
	"net/url"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/tracetools"
)

// LocalJobLogsConfig records explicit operator choices before the agent
// removes configuration environment variables. Explicit false is an opt-out.
type LocalJobLogsConfig struct {
	EnabledSet         bool
	OTLPDestinationSet bool
}

// ApplyControlPlaneOTel prefers shared OTel policy over legacy tracing, even
// when disabled. Disabled policy leaves explicit local configuration intact.
func ApplyControlPlaneOTel(l logger.Logger, conf *AgentConfiguration, reg *api.AgentRegisterResponse, tracing LocalTracingConfig, logs LocalJobLogsConfig) {
	if reg.OTel == nil {
		ApplyControlPlaneTracing(l, conf, reg.Tracing, tracing)
		return
	}
	if !reg.OTel.Enabled {
		return
	}
	ApplyControlPlaneTracing(l, conf, &api.AgentTracing{
		Backend:  tracetools.BackendOpenTelemetry,
		Exporter: signalExporter(reg.OTel.Exporter, "traces"),
	}, tracing)
	if reg.OTel.JobLogs {
		applyControlPlaneJobLogs(l, conf, signalExporter(reg.OTel.Exporter, "logs"), logs)
	}
}

func signalExporter(exporter *api.OTLPExporter, signal string) *api.OTLPExporter {
	if exporter == nil || exporter.Endpoint == "" {
		return nil
	}
	endpoint, err := url.JoinPath(exporter.Endpoint, "v1", signal)
	if err != nil {
		return nil
	}
	copy := *exporter
	copy.Endpoint = endpoint
	return &copy
}

// applyControlPlaneJobLogs preserves local opt-outs and whole destinations.
func applyControlPlaneJobLogs(l logger.Logger, conf *AgentConfiguration, exporter *api.OTLPExporter, local LocalJobLogsConfig) {
	if local.EnabledSet && !conf.JobLogsOTLP {
		l.Infof("Ignoring control-plane job logs: explicitly disabled locally")
		return
	}
	conf.JobLogsOTLP = true
	if local.OTLPDestinationSet {
		l.Infof("Control-plane job logs enabled; exporter ignored (local OTLP destination takes precedence)")
		return
	}
	if exporter != nil && exporter.Endpoint != "" {
		conf.ControlPlaneJobLogsExporter = exporter
		l.Infof("Control-plane job logs enabled: exporter=%s", sanitizedEndpoint(exporter.Endpoint))
	}
}
