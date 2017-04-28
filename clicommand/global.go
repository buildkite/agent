package clicommand

import (
	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/experiments"
	"github.com/urfave/cli"
	"github.com/oleiade/reflections"
)

const (
	DefaultEndpoint = "https://agent.buildkite.com/v3"
)

var AgentAccessTokenFlag = cli.StringFlag{
	Name:   "agent-access-token",
	Value:  "",
	Usage:  "The access token used to identify the agent",
	EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
}

var EndpointFlag = cli.StringFlag{
	Name:   "endpoint",
	Value:  DefaultEndpoint,
	Usage:  "The Agent API endpoint",
	EnvVar: "BUILDKITE_AGENT_ENDPOINT",
}

var DebugFlag = cli.BoolFlag{
	Name:   "debug",
	Usage:  "Enable debug mode",
	EnvVar: "BUILDKITE_AGENT_DEBUG",
}

var DebugHTTPFlag = cli.BoolFlag{
	Name:   "debug-http",
	Usage:  "Enable HTTP debug mode, which dumps all request and response bodies to the log",
	EnvVar: "BUILDKITE_AGENT_DEBUG_HTTP",
}

var NoColorFlag = cli.BoolFlag{
	Name:   "no-color",
	Usage:  "Don't show colors in logging",
	EnvVar: "BUILDKITE_AGENT_NO_COLOR",
}

var ExperimentsFlag = cli.StringSliceFlag{
	Name:   "experiment",
	Value:  &cli.StringSlice{},
	Usage:  "Enable experimental features within the buildkite-agent",
	EnvVar: "BUILDKITE_AGENT_EXPERIMENT",
}

func HandleGlobalFlags(cfg interface{}) {
	// Enable debugging if a Debug option is present
	debug, err := reflections.GetField(cfg, "Debug")
	if debug == true && err == nil {
		logger.SetLevel(logger.DEBUG)
	}

	// Enable HTTP debugging
	debugHTTP, err := reflections.GetField(cfg, "DebugHTTP")
	if debugHTTP == true && err == nil {
		agent.APIClientEnableHTTPDebug()
	}

	// Turn off color if a NoColor option is present
	noColor, err := reflections.GetField(cfg, "NoColor")
	if noColor == true && err == nil {
		logger.SetColors(false)
	}

	// Enable experiments
	experimentNames, err := reflections.GetField(cfg, "Experiments")
	if err == nil {
		experimentNamesSlice, ok := experimentNames.([]string)
		if ok {
			for _, name := range experimentNamesSlice {
				experiments.Enable(name)
			}
		}
	}
}
