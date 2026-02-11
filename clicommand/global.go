package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/oleiade/reflections"
	"github.com/urfave/cli"
)

const (
	DefaultEndpoint = "https://agent.buildkite.com/v3"
)

var (
	AgentAccessTokenFlag = cli.StringFlag{
		Name:   "agent-access-token",
		Value:  "",
		Usage:  "The access token used to identify the agent",
		EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
	}

	AgentRegisterTokenFlag = cli.StringFlag{
		Name:   "token",
		Value:  "",
		Usage:  "Your account agent token",
		EnvVar: "BUILDKITE_AGENT_TOKEN",
	}

	EndpointFlag = cli.StringFlag{
		Name:   "endpoint",
		Value:  DefaultEndpoint,
		Usage:  "The Agent API endpoint",
		EnvVar: "BUILDKITE_AGENT_ENDPOINT",
	}

	NoHTTP2Flag = cli.BoolFlag{
		Name:   "no-http2",
		Usage:  "Disable HTTP2 when communicating with the Agent API (default: false)",
		EnvVar: "BUILDKITE_NO_HTTP2",
	}

	DebugFlag = cli.BoolFlag{
		Name:   "debug",
		Usage:  "Enable debug mode. Synonym for ′--log-level debug′. Takes precedence over ′--log-level′ (default: false)",
		EnvVar: "BUILDKITE_AGENT_DEBUG",
	}

	LogLevelFlag = cli.StringFlag{
		Name:   "log-level",
		Value:  "notice",
		Usage:  "Set the log level for the agent, making logging more or less verbose. Defaults to notice. Allowed values are: debug, info, error, warn, fatal",
		EnvVar: "BUILDKITE_AGENT_LOG_LEVEL",
	}

	ProfileFlag = cli.StringFlag{
		Name:   "profile",
		Usage:  "Enable a profiling mode, either cpu, memory, mutex or block",
		EnvVar: "BUILDKITE_AGENT_PROFILE",
	}

	DebugHTTPFlag = cli.BoolFlag{
		Name:   "debug-http",
		Usage:  "Enable HTTP debug mode, which dumps all request and response bodies to the log (default: false)",
		EnvVar: "BUILDKITE_AGENT_DEBUG_HTTP",
	}

	TraceHTTPFlag = cli.BoolFlag{
		Name:   "trace-http",
		Usage:  "Enable HTTP trace mode, which logs timings for each HTTP request. Timings are logged at the debug level unless a request fails at the network level in which case they are logged at the error level (default: false)",
		EnvVar: "BUILDKITE_AGENT_TRACE_HTTP",
	}

	NoColorFlag = cli.BoolFlag{
		Name:   "no-color",
		Usage:  "Don't show colors in logging (default: false)",
		EnvVar: "BUILDKITE_AGENT_NO_COLOR",
	}

	StrictSingleHooksFlag = cli.BoolFlag{
		Name:   "strict-single-hooks",
		Usage:  "Enforces that only one checkout hook, and only one command hook, can be run (default: false)",
		EnvVar: "BUILDKITE_STRICT_SINGLE_HOOKS",
	}

	SocketsPathFlag = cli.StringFlag{
		Name:   "sockets-path",
		Value:  defaultSocketsPath(),
		Usage:  "Directory where the agent will place sockets",
		EnvVar: "BUILDKITE_SOCKETS_PATH",
	}

	KubernetesContainerIDFlag = cli.IntFlag{
		Name: "kubernetes-container-id",
		Usage: "This is intended to be used only by the Buildkite k8s stack " +
			"(github.com/buildkite/agent-stack-k8s); it sets an ID number " +
			"used to identify this container within the pod",
		EnvVar: "BUILDKITE_CONTAINER_ID",
	}

	KubernetesLogCollectionGracePeriodFlag = cli.DurationFlag{
		Name:   "kubernetes-log-collection-grace-period",
		Usage:  "Deprecated, do not use",
		EnvVar: "BUILDKITE_KUBERNETES_LOG_COLLECTION_GRACE_PERIOD",
		Value:  50 * time.Second,
	}

	NoMultipartArtifactUploadFlag = cli.BoolFlag{
		Name:   "no-multipart-artifact-upload",
		Usage:  "For Buildkite-hosted artifacts, disables the use of multipart uploads. Has no effect on uploads to other destinations such as custom cloud buckets (default: false)",
		EnvVar: "BUILDKITE_NO_MULTIPART_ARTIFACT_UPLOAD",
	}

	ExperimentsFlag = cli.StringSliceFlag{
		Name:   "experiment",
		Value:  &cli.StringSlice{},
		Usage:  "Enable experimental features within the buildkite-agent",
		EnvVar: "BUILDKITE_AGENT_EXPERIMENT",
	}

	GzipAPIRequestsFlag = cli.BoolFlag{
		Name:   "gzip-api-requests",
		Usage:  "Enable gzip compression for API request bodies",
		EnvVar: "BUILDKITE_GZIP_API_REQUESTS",
	}

	RedactedVars = cli.StringSliceFlag{
		Name:   "redacted-vars",
		Usage:  "Pattern of environment variable names containing sensitive values",
		EnvVar: "BUILDKITE_REDACTED_VARS",
		Value: &cli.StringSlice{
			"*_PASSWORD",
			"*_SECRET",
			"*_TOKEN",
			"*_PRIVATE_KEY",
			"*_ACCESS_KEY",
			"*_SECRET_KEY",
			// Connection strings frequently contain passwords, e.g.
			// https://user:pass@host/ or Server=foo;Database=my-db;User Id=user;Password=pass;
			"*_CONNECTION_STRING",
		},
	}

	TraceContextEncodingFlag = cli.StringFlag{
		Name:   "trace-context-encoding",
		Usage:  "Sets the inner encoding for BUILDKITE_TRACE_CONTEXT. Must be either json or gob",
		Value:  "gob",
		EnvVar: "BUILDKITE_TRACE_CONTEXT_ENCODING",
	}
)

// GlobalConfig includes very common shared config options for easy inclusion across
// config structs (via embedding).
type GlobalConfig struct {
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

// APIConfig includes API-related shared options for easy inclusion across
// config structs (via embedding). Subcommands that don't need APIConfig usually
// do something "trivial" (e.g. acknowledgements) or "special" (e.g. start).
type APIConfig struct {
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	DebugHTTP        bool   `cli:"debug-http"`
	TraceHTTP        bool   `cli:"trace-http"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
	GzipAPIRequests  bool   `cli:"gzip-api-requests"`
}

func globalFlags() []cli.Flag {
	return []cli.Flag{
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	}
}

func apiFlags() []cli.Flag {
	return []cli.Flag{
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,
		TraceHTTPFlag,
		GzipAPIRequestsFlag,
	}
}

func CreateLogger(cfg any) logger.Logger {
	var l logger.Logger
	logFormat := "text"

	// Check the LogFormat config field
	if logFormatCfg, err := reflections.GetField(cfg, "LogFormat"); err == nil {
		if logFormatString, ok := logFormatCfg.(string); ok {
			logFormat = logFormatString
		}
	}

	// Create a logger based on the type
	switch logFormat {
	case "text", "":
		printer := logger.NewTextPrinter(os.Stderr)

		// Show agent fields as a prefix
		printer.IsPrefixFn = func(field logger.Field) bool {
			switch field.Key() {
			case "agent", "hook":
				return true
			default:
				return false
			}
		}

		// Turn off color if a NoColor option is present
		noColor, err := reflections.GetField(cfg, "NoColor")
		if noColor == true && err == nil {
			printer.Colors = false
		} else {
			printer.Colors = true
		}

		l = logger.NewConsoleLogger(printer, os.Exit)
	case "json":
		l = logger.NewConsoleLogger(logger.NewJSONPrinter(os.Stdout), os.Exit)
	default:
		fmt.Printf("Unknown log-format of %q, try text or json\n", logFormat)
		os.Exit(1)
	}

	l.SetLevel(logger.NOTICE)

	err := handleLogLevelFlag(l, cfg)
	if err != nil {
		l.Warn("Error when setting log level: %v. Defaulting log level to NOTICE", err)
	}

	// Enable debugging if a Debug option is present
	debugI, _ := reflections.GetField(cfg, "Debug")
	if debug, ok := debugI.(bool); ok && debug {
		l.SetLevel(logger.DEBUG)
	}

	return l
}

func HandleProfileFlag(l logger.Logger, cfg any) func() {
	// Enable profiling a profiling mode if Profile is present
	modeField, _ := reflections.GetField(cfg, "Profile")
	if mode, ok := modeField.(string); ok && mode != "" {
		return Profile(l, mode)
	}
	return func() {}
}

func HandleGlobalFlags(ctx context.Context, l logger.Logger, cfg any) (context.Context, func()) {
	// Enable experiments
	experimentNames, err := reflections.GetField(cfg, "Experiments")
	if err != nil {
		return ctx, HandleProfileFlag(l, cfg)
	}

	experimentNamesSlice, ok := experimentNames.([]string)
	if !ok {
		return ctx, HandleProfileFlag(l, cfg)
	}

	for _, name := range experimentNamesSlice {
		nctx, state := experiments.EnableWithWarnings(ctx, l, name)
		if state == experiments.StateKnown {
			l.Debug("Enabled experiment %q", name)
		}
		ctx = nctx
	}

	// Handle profiling flag
	return ctx, HandleProfileFlag(l, cfg)
}

func handleLogLevelFlag(l logger.Logger, cfg any) error {
	logLevel, err := reflections.GetField(cfg, "LogLevel")
	if err != nil {
		return err
	}

	llStr, ok := logLevel.(string)
	if !ok {
		return fmt.Errorf("log level %v (%T) couldn't be cast to string", logLevel, logLevel)
	}

	level, err := logger.LevelFromString(llStr)
	if err != nil {
		return err
	}

	l.SetLevel(level)
	return nil
}

func UnsetConfigFromEnvironment(c *cli.Context) error {
	flags := append(c.App.Flags, c.Command.Flags...)
	for _, fl := range flags {
		// use golang reflection to find EnvVar values on flags
		r := reflect.ValueOf(fl)
		f := reflect.Indirect(r).FieldByName("EnvVar")
		if !f.IsValid() {
			return errors.New("EnvVar field not found on flag")
		}
		// split comma delimited env
		if envVars := f.String(); envVars != "" {
			for env := range strings.SplitSeq(envVars, ",") {
				os.Unsetenv(env)
			}
		}
	}
	return nil
}

func loadAPIClientConfig(cfg any, tokenField string) api.Config {
	conf := api.Config{
		UserAgent: version.UserAgent(),
	}

	// Enable HTTP debugging
	debugHTTP, err := reflections.GetField(cfg, "DebugHTTP")
	if debugHTTP == true && err == nil {
		conf.DebugHTTP = true
	}

	traceHTTP, err := reflections.GetField(cfg, "TraceHTTP")
	if traceHTTP == true && err == nil {
		conf.TraceHTTP = true
	}

	endpoint, err := reflections.GetField(cfg, "Endpoint")
	if endpoint != "" && err == nil {
		conf.Endpoint = endpoint.(string)
	}

	token, err := reflections.GetField(cfg, tokenField)
	if token != "" && err == nil {
		conf.Token = token.(string)
	}

	noHTTP2, err := reflections.GetField(cfg, "NoHTTP2")
	if err == nil {
		conf.DisableHTTP2 = noHTTP2.(bool)
	}

	gzipAPIRequests, err := reflections.GetField(cfg, "GzipAPIRequests")
	if err == nil {
		conf.GzipAPIRequests = gzipAPIRequests.(bool)
	}

	return conf
}

type configOpts func(*cliconfig.Loader)

func withConfigFilePaths(paths []string) func(*cliconfig.Loader) {
	return func(loader *cliconfig.Loader) {
		loader.DefaultConfigFilePaths = paths
	}
}

// setupLoggerAndConfig populates the given config struct with values from the
// CLI flags and environment variables. It returns the config struct, a logger
// based on the resulting config, a reference to the config file (if any), and
// a function, which must be deferred.
//
// Presently, the returned function will wind down the profiler, which is only
// optionally started, based on the config. However, it may be extended in the
// future to clean up other resources. Importantly, the calling code does not
// need to know or care about what the returned function does, only that it
// must defer it.
func setupLoggerAndConfig[T any](ctx context.Context, c *cli.Context, opts ...configOpts) (
	newCtx context.Context,
	cfg T,
	l logger.Logger,
	f *cliconfig.File,
	done func(),
) {
	loader := cliconfig.Loader{CLI: c, Config: &cfg}

	for _, opt := range opts {
		opt(&loader)
	}

	warnings, err := loader.Load()
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "%s\n", err)
		os.Exit(1)
	}

	l = CreateLogger(&cfg)

	if debug, err := reflections.GetField(cfg, "Debug"); err == nil && debug.(bool) {
		l = l.WithFields(logger.StringField("command", c.Command.FullName()))
	}

	l.Debug("Loaded config")

	// Now that we have a logger, log out the warnings that loading config generated
	for _, warning := range warnings {
		l.Warn("%s", warning)
	}

	// Setup any global configuration options
	ctx, done = HandleGlobalFlags(ctx, l, cfg)
	return ctx, cfg, l, loader.File, done
}
