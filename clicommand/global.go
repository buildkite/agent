package clicommand

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/experiments"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/stdin"
	"github.com/oleiade/reflections"
	"github.com/urfave/cli"
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

// HandlePipelineFileFlag reads either a provided file path, stdin, or searches for a pipeline file
func HandlePipelineFileFlag(filePath string) (filename string, content []byte, err error) {
	// first try the provided path
	if filePath != "" {
		logger.Info("Reading pipeline config from \"%s\"", filePath)

		filename = filepath.Base(filePath)
		content, err = ioutil.ReadFile(filePath)
		if err != nil {
			return "", nil, fmt.Errorf("Failed to read file: %s", err)
		}

		return filename, content, nil
	}

	// next try stdin
	if stdin.IsReadable() {
		logger.Info("Reading pipeline config from STDIN")

		// Actually read the file from STDIN
		content, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			return "", nil, fmt.Errorf("Failed to read from STDIN: %s", err)
		}

		return filename, content, nil
	}

	// otherwise search for a pipeline config
	logger.Info("Searching for pipeline config...")

	// Collect all pipeline files that exist
	exists := []string{}
	for _, path := range []string{
		"buildkite.yml",
		"buildkite.yaml",
		"buildkite.json",
		filepath.Join(".buildkite", "pipeline.yml"),
		filepath.Join(".buildkite", "pipeline.yaml"),
		filepath.Join(".buildkite", "pipeline.json"),
	} {
		if _, err := os.Stat(path); err == nil {
			exists = append(exists, path)
		}
	}

	// If more than 1 of the config files exist, throw an
	// error. There can only be one!!
	if len(exists) > 1 {
		return "", nil, fmt.Errorf("Found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
	} else if len(exists) == 0 {
		return "", nil, fmt.Errorf("Could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
	}

	found := exists[0]
	logger.Info("Found config file \"%s\"", found)

	// Read the default file
	filename = path.Base(found)
	content, err = ioutil.ReadFile(found)
	if err != nil {
		return "", nil, fmt.Errorf("Failed to read file \"%s\": %v", found, err)
	}

	// Make sure the file actually has something in it
	if len(content) == 0 {
		return "", nil, fmt.Errorf("Config file %q is empty", found)
	}

	return filename, content, nil
}
