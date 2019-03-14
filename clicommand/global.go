package clicommand

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/experiments"
	"github.com/buildkite/agent/logger"
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

var AgentRegisterTokenFlag = cli.StringFlag{
	Name:   "token",
	Value:  "",
	Usage:  "Your account agent token",
	EnvVar: "BUILDKITE_AGENT_TOKEN",
}

var EndpointFlag = cli.StringFlag{
	Name:   "endpoint",
	Value:  DefaultEndpoint,
	Usage:  "The Agent API endpoint",
	EnvVar: "BUILDKITE_AGENT_ENDPOINT",
}

var NoHTTP2Flag = cli.BoolFlag{
	Name:   "no-http2",
	Usage:  "Disable HTTP2 when communicating with the Agent API.",
	EnvVar: "BUILDKITE_NO_HTTP2",
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

var DebugWithoutAPIFlag = cli.BoolFlag{
	Name:   "debug-without-api",
	Usage:  "Enable debug mode, except for the API client",
	Hidden: true,
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

func HandleGlobalFlags(l *logger.Logger, cfg interface{}) {
	// Enable debugging, but disable the api client
	debugWithoutAPI, err := reflections.GetField(cfg, "DebugWithoutAPI")
	if debugWithoutAPI == true && err == nil {
		agent.APIClientDisableDebug()
	}

	// Enable debugging if a Debug option is present
	debug, _ := reflections.GetField(cfg, "Debug")
	if debug == false && debugWithoutAPI == false {
		l.Level = logger.INFO
	}

	// Turn off color if a NoColor option is present
	noColor, err := reflections.GetField(cfg, "NoColor")
	if noColor == true && err == nil {
		l.Colors = false
	} else {
		l.Colors = true
	}

	// Enable experiments
	experimentNames, err := reflections.GetField(cfg, "Experiments")
	if err == nil {
		experimentNamesSlice, ok := experimentNames.([]string)
		if ok {
			for _, name := range experimentNamesSlice {
				experiments.Enable(name)
				l.Debug("Enabled experiment `%s`", name)
			}
		}
	}
}

func UnsetConfigFromEnvironment(c *cli.Context) {
	flags := append(c.App.Flags, c.Command.Flags...)
	for _, fl := range flags {
		// use golang reflection to find EnvVar values on flags
		r := reflect.ValueOf(fl)
		f := reflect.Indirect(r).FieldByName(`EnvVar`)
		// split comma delimited env
		if envVars := f.String(); envVars != `` {
			for _, env := range strings.Split(envVars, ",") {
				os.Unsetenv(env)
			}
		}
	}
}

func DefaultShell() string {
	// https://github.com/golang/go/blob/master/src/go/build/syslist.go#L7
	switch runtime.GOOS {
	case "windows":
		return `C:\Windows\System32\CMD.exe /S /C`
	case "freebsd", "openbsd", "netbsd":
		return `/usr/local/bin/bash -e -c`
	default:
		return `/bin/bash -e -c`
	}
}

func DefaultConfigFilePaths() (paths []string) {
	// Toggle beetwen windows an *nix paths
	if runtime.GOOS == "windows" {
		paths = []string{
			"C:\\buildkite-agent\\buildkite-agent.cfg",
			"$USERPROFILE\\AppData\\Local\\buildkite-agent\\buildkite-agent.cfg",
			"$USERPROFILE\\AppData\\Local\\BuildkiteAgent\\buildkite-agent.cfg",
		}
	} else {
		paths = []string{
			"$HOME/.buildkite-agent/buildkite-agent.cfg",
			"/usr/local/etc/buildkite-agent/buildkite-agent.cfg",
			"/etc/buildkite-agent/buildkite-agent.cfg",
		}
	}

	// Also check to see if there's a buildkite-agent.cfg in the folder
	// that the binary is running in.
	pathToBinary, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err == nil {
		pathToRelativeConfig := filepath.Join(pathToBinary, "buildkite-agent.cfg")
		paths = append([]string{pathToRelativeConfig}, paths...)
	}

	return
}

func loadAPIClientConfig(cfg interface{}, tokenField string) agent.APIClientConfig {
	// Enable HTTP debugging
	debugHTTP, err := reflections.GetField(cfg, "DebugHTTP")
	if debugHTTP == true && err == nil {
		agent.APIClientEnableHTTPDebug()
	}

	var a agent.APIClientConfig

	endpoint, err := reflections.GetField(cfg, "Endpoint")
	if endpoint != "" && err == nil {
		a.Endpoint = endpoint.(string)
	}

	token, err := reflections.GetField(cfg, tokenField)
	if token != "" && err == nil {
		a.Token = token.(string)
	}

	noHTTP2, err := reflections.GetField(cfg, "NoHTTP2")
	if err == nil {
		a.DisableHTTP2 = noHTTP2.(bool)
	}

	return a
}

func LoadConfigAndGlobalFlags(l *logger.Logger, c *cli.Context, cfg interface{}) (string, error) {
	// Setup the config loader. You'll see that we also path paths to
	// potential config files. The loader will use the first one it finds.
	loader := cliconfig.Loader{
		CLI:                    c,
		Config:                 cfg,
		DefaultConfigFilePaths: DefaultConfigFilePaths(),
		Logger:                 l,
	}

	// Load the configuration
	if err := loader.Load(); err != nil {
		return "", err
	}

	// Setup the any global configuration options
	HandleGlobalFlags(l, cfg)

	if loader.File != nil {
		return loader.File.Path, nil
	}

	return "", nil
}

func parseNilableDuration(ds string) (time.Duration, error) {
	var d time.Duration
	if ds != "" {
		var err error
		d, err = time.ParseDuration(ds)
		if err != nil {
			return d, err
		}
	}
	return d, nil
}
