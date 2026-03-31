package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/cliconfig"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/version"
	"github.com/oleiade/reflections"
	"github.com/urfave/cli"
)

const (
	DefaultEndpoint = "https://agent-edge.buildkite.com/v3"
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
			"*_API_KEY",
		},
	}

	TraceContextEncodingFlag = cli.StringFlag{
		Name:   "trace-context-encoding",
		Usage:  "Sets the inner encoding for BUILDKITE_TRACE_CONTEXT. Must be either json or gob",
		Value:  "gob",
		EnvVar: "BUILDKITE_TRACE_CONTEXT_ENCODING",
	}
)

// File path flags shared between agent start and bootstrap
var (
	BuildPathFlag = cli.StringFlag{
		Name:   "build-path",
		Value:  "",
		Usage:  "Path to where the builds will run from",
		EnvVar: "BUILDKITE_BUILD_PATH",
	}

	HooksPathFlag = cli.StringFlag{
		Name:   "hooks-path",
		Value:  "",
		Usage:  "Directory where the hook scripts are found",
		EnvVar: "BUILDKITE_HOOKS_PATH",
	}

	AdditionalHooksPathsFlag = cli.StringSliceFlag{
		Name:   "additional-hooks-paths",
		Value:  &cli.StringSlice{},
		Usage:  "Additional directories to look for agent hooks",
		EnvVar: "BUILDKITE_ADDITIONAL_HOOKS_PATHS",
	}

	SocketsPathFlag = cli.StringFlag{
		Name:   "sockets-path",
		Value:  defaultSocketsPath(),
		Usage:  "Directory where the agent will place sockets",
		EnvVar: "BUILDKITE_SOCKETS_PATH",
	}

	PluginsPathFlag = cli.StringFlag{
		Name:   "plugins-path",
		Value:  "",
		Usage:  "Directory where the plugins are saved to",
		EnvVar: "BUILDKITE_PLUGINS_PATH",
	}
)

// Git related flags shared between agent start and bootstrap
var (
	SkipCheckoutFlag = cli.BoolFlag{
		Name:   "skip-checkout",
		Usage:  "Skip the git checkout phase entirely",
		EnvVar: "BUILDKITE_SKIP_CHECKOUT",
	}

	GitCheckoutFlagsFlag = cli.StringFlag{
		Name:   "git-checkout-flags",
		Value:  "-f",
		Usage:  "Flags to pass to \"git checkout\" command",
		EnvVar: "BUILDKITE_GIT_CHECKOUT_FLAGS",
	}

	GitCloneFlagsFlag = cli.StringFlag{
		Name:   "git-clone-flags",
		Value:  "-v",
		Usage:  "Flags to pass to \"git clone\" command",
		EnvVar: "BUILDKITE_GIT_CLONE_FLAGS",
	}

	GitCloneMirrorFlagsFlag = cli.StringFlag{
		Name:   "git-clone-mirror-flags",
		Value:  "-v",
		Usage:  "Flags to pass to \"git clone\" command when mirroring",
		EnvVar: "BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
	}

	GitCleanFlagsFlag = cli.StringFlag{
		Name:   "git-clean-flags",
		Value:  "-ffxdq",
		Usage:  "Flags to pass to \"git clean\" command",
		EnvVar: "BUILDKITE_GIT_CLEAN_FLAGS",
		// -ff: delete files and directories, including untracked nested git repositories
		// -x: don't use .gitignore rules
		// -d: recurse into untracked directories
		// -q: quiet, only report errors
	}

	GitFetchFlagsFlag = cli.StringFlag{
		Name:   "git-fetch-flags",
		Value:  "-v --prune",
		Usage:  "Flags to pass to \"git fetch\" command",
		EnvVar: "BUILDKITE_GIT_FETCH_FLAGS",
	}

	GitMirrorsPathFlag = cli.StringFlag{
		Name:   "git-mirrors-path",
		Value:  "",
		Usage:  "Path to where mirrors of git repositories are stored",
		EnvVar: "BUILDKITE_GIT_MIRRORS_PATH",
	}

	GitMirrorCheckoutModeFlag = cli.StringFlag{
		Name:   "git-mirror-checkout-mode",
		Value:  "reference",
		Usage:  fmt.Sprintf("Changes how clones of a mirror are made; available modes are %v. In ′dissociate′ mode, clones from a mirror uses the git clone ′--dissociate′ flag, which copies underlying objects from the mirror, making the clone robust to changes in the mirror such as garbage collection, at the expense of additional disk usage and setup time. ′reference′ mode does not pass ′--dissociate′, which causes the clone to directly use objects from the mirror, which is more fragile and can cause the clone to break under entirely normal operation of the mirror, but is slightly faster to clone and uses less disk space.", mirrorCheckoutModes),
		EnvVar: "BUILDKITE_GIT_MIRROR_CHECKOUT_MODE",
	}

	GitMirrorsLockTimeoutFlag = cli.IntFlag{
		Name:   "git-mirrors-lock-timeout",
		Value:  300,
		Usage:  "Seconds to lock a git mirror during clone, should exceed your longest checkout",
		EnvVar: "BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
	}

	GitMirrorsSkipUpdateFlag = cli.BoolFlag{
		Name:   "git-mirrors-skip-update",
		Usage:  "Skip updating the Git mirror (default: false)",
		EnvVar: "BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
	}

	GitSubmoduleCloneConfigFlag = cli.StringSliceFlag{
		Name:   "git-submodule-clone-config",
		Value:  &cli.StringSlice{},
		Usage:  "Comma separated key=value git config pairs applied before git submodule clone commands such as ′update --init′. If the config is needed to be applied to all git commands, supply it in a global git config file for the system that the agent runs in instead",
		EnvVar: "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG",
	}

	GitSkipFetchExistingCommitsFlag = cli.BoolFlag{
		Name:   "git-skip-fetch-existing-commits",
		Usage:  "Skip git fetch if the commit already exists in the local git directory (default: false)",
		EnvVar: "BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS",
	}

	CheckoutAttemptsFlag = cli.IntFlag{
		Name:   "checkout-attempts",
		Value:  6,
		Usage:  "Number of checkout attempts (including the initial attempt). Failed attempts are retried with exponential backoff (factor of 2, starting at 1s: 1s, 2s, 4s, ...)",
		EnvVar: "BUILDKITE_CHECKOUT_ATTEMPTS",
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
