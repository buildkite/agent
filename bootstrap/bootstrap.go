package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/retry"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/utils"
	"github.com/buildkite/shellwords"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/pkg/errors"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/opentracer"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// RedactLengthMin is the shortest string length that will be considered a
// potential secret by the environment redactor. e.g. if the redactor is
// configured to filter out environment variables matching *_TOKEN, and
// API_TOKEN is set to "none", this minimum length will prevent the word "none"
// from being redacted from useful log output.
const RedactLengthMin = 6

// Bootstrap represents the phases of execution in a Buildkite Job. It's run
// as a sub-process of the buildkite-agent and finishes at the conclusion of a job.
// Historically (prior to v3) the bootstrap was a shell script, but was ported to
// Golang for portability and testability
type Bootstrap struct {
	// Config provides the bootstrap configuration
	Config

	// Shell is the shell environment for the bootstrap
	shell *shell.Shell

	// Plugins to use
	plugins []*plugin.Plugin

	// Plugin checkouts from the plugin phases
	pluginCheckouts []*pluginCheckout

	// Directories to clean up at end of bootstrap
	cleanupDirs []string

	// A channel to track cancellation
	cancelCh chan struct{}
}

// New returns a new Bootstrap instance
func New(conf Config) *Bootstrap {
	return &Bootstrap{
		Config:   conf,
		cancelCh: make(chan struct{}),
	}
}

// Run the bootstrap and return the exit code
func (b *Bootstrap) Run(ctx context.Context) (exitCode int) {
	// Check if not nil to allow for tests to overwrite shell
	if b.shell == nil {
		var err error
		b.shell, err = shell.NewWithContext(ctx)
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			return 1
		}

		b.shell.PTY = b.Config.RunInPty
		b.shell.Debug = b.Config.Debug
	}

	// Listen for cancellation
	go func() {
		select {
		case <-ctx.Done():
			return

		case <-b.cancelCh:
			b.shell.Commentf("Received cancellation signal, interrupting")
			b.shell.Interrupt()
		}
	}()

	span, ctx, stopper := b.startTracing(ctx)
	defer stopper()
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		if err = b.tearDown(ctx); err != nil {
			b.shell.Errorf("Error tearing down bootstrap: %v", err)

			// this gets passed back via the named return
			exitCode = shell.GetExitCode(err)
		}
	}()

	// Initialize the environment, a failure here will still call the tearDown
	if err = b.setUp(ctx); err != nil {
		b.shell.Errorf("Error setting up bootstrap: %v", err)
		return shell.GetExitCode(err)
	}

	var includePhase = func(phase string) bool {
		if len(b.Phases) == 0 {
			return true
		}
		for _, include := range b.Phases {
			if include == phase {
				return true
			}
		}
		return false
	}

	//  Execute the bootstrap phases in order
	var phaseErr error

	if includePhase(`plugin`) {
		phaseErr = b.preparePlugins()

		if phaseErr == nil {
			phaseErr = b.PluginPhase(ctx)
		}
	}

	if phaseErr == nil && includePhase(`checkout`) {
		phaseErr = b.CheckoutPhase(ctx)
	} else {
		checkoutDir, exists := b.shell.Env.Get(`BUILDKITE_BUILD_CHECKOUT_PATH`)
		if exists {
			_ = b.shell.Chdir(checkoutDir)
		}
	}

	if phaseErr == nil && includePhase(`plugin`) {
		phaseErr = b.VendoredPluginPhase(ctx)
	}

	if phaseErr == nil && includePhase(`command`) {
		var commandErr error
		phaseErr, commandErr = b.CommandPhase(ctx)
		// Add command exit error info. This is distinct from a phaseErr, which is
		// an error from the hook/job logic. These are both good to report but
		// shouldn't override each other in reporting.
		if commandErr != nil {
			b.shell.Printf("user command error: %v", commandErr)
			ext.LogError(span, commandErr)
		}

		// Only upload artifacts as part of the command phase
		if err = b.uploadArtifacts(ctx); err != nil {
			b.shell.Errorf("%v", err)
			return shell.GetExitCode(err)
		}
	}

	// Phase errors are where something of ours broke that merits a big red error
	// this won't include command failures, as we view that as more in the user space
	if phaseErr != nil {
		err = phaseErr
		b.shell.Errorf("%v", phaseErr)
		return shell.GetExitCode(phaseErr)
	}

	// Use the exit code from the command phase
	exitStatus, _ := b.shell.Env.Get(`BUILDKITE_COMMAND_EXIT_STATUS`)
	exitStatusCode, _ := strconv.Atoi(exitStatus)

	return exitStatusCode
}

// Cancel interrupts any running shell processes and causes the bootstrap to stop
func (b *Bootstrap) Cancel() error {
	b.cancelCh <- struct{}{}
	return nil
}

// extractTraceCtx pulls encoded distributed tracing information from the env vars.
// Note: This should match the injectTraceCtx code in shell.
func (b *Bootstrap) extractTraceCtx() opentracing.SpanContext {
	sctx, err := tracetools.DecodeTraceContext(b.shell.Env.ToMap())
	if err != nil {
		// Return nil so a new span will be created
		return nil
	} else {
		return sctx
	}
}

// stopper lets us abstract the tracer wrap up code so we can plug in different tracing
// library implementations that are opentracing compatible. Opentracing itself
// doesn't have a Stop function on its Tracer interface.
type stopper func()

// startTracing sets up tracing based on the config values. It uses opentracing as an
// abstraction so the agent can support multiple libraries if needbe.
func (b *Bootstrap) startTracing(ctx context.Context) (opentracing.Span, context.Context, stopper) {
	// Newer versions of the tracing libs print out diagnostic info which spams the
	// Buildkite agent logs. Disable it by default unless it's been explicitly set.
	if _, has := os.LookupEnv("DD_TRACE_STARTUP_LOGS"); !has {
		os.Setenv("DD_TRACE_STARTUP_LOGS", "false")
	}

	buildID, _ := b.shell.Env.Get("BUILDKITE_BUILD_ID")
	buildNumber, _ := b.shell.Env.Get("BUILDKITE_BUILD_NUMBER")
	buildURL, _ := b.shell.Env.Get("BUILDKITE_BUILD_URL")
	jobURL := fmt.Sprintf("%s#%s", buildURL, b.JobID)
	source, _ := b.shell.Env.Get("BUILDKITE_SOURCE")
	label, hasLabel := b.shell.Env.Get("BUILDKITE_LABEL")
	if !hasLabel {
		label = "job"
	}
	retry := int64(0)
	if attemptStr, has := b.shell.Env.Get("BUILDKITE_RETRY_COUNT"); has {
		if parsedRetry, err := strconv.ParseInt(attemptStr, 10, 64); err == nil {
			retry = parsedRetry
		}
	}
	parallel := int64(0)
	if parallelStr, has := b.shell.Env.Get("BUILDKITE_PARALLEL_JOB"); has {
		if parsedParallel, err := strconv.ParseInt(parallelStr, 10, 64); err == nil {
			parallel = parsedParallel
		}
	}
	rebuiltFromID, has := b.shell.Env.Get("BUILDKITE_REBUILT_FROM_BUILD_NUMBER")
	if !has || rebuiltFromID == "" {
		rebuiltFromID = "n/a"
	}
	triggeredFromID, has := b.shell.Env.Get("BUILDKITE_TRIGGERED_FROM_BUILD_ID")
	if !has || triggeredFromID == "" {
		triggeredFromID = "n/a"
	}

	// Set specific tracing library here. Everything else should be using opentracing.
	// Use a constant sampler - CI runs aren't high traffic.
	var t opentracing.Tracer
	var stopper stopper
	switch b.Config.TracingBackend {
	case "datadog":
		t = opentracer.New(
			tracer.WithServiceName("buildkite_agent"),
			tracer.WithSampler(tracer.NewAllSampler()),
			tracer.WithAnalytics(true),
			tracer.WithGlobalTag("buildkite.agent", b.AgentName),
			tracer.WithGlobalTag("buildkite.version", agent.Version()),
			tracer.WithGlobalTag("buildkite.queue", b.Queue),
			tracer.WithGlobalTag("buildkite.org", b.OrganizationSlug),
			tracer.WithGlobalTag("buildkite.pipeline", b.PipelineSlug),
			tracer.WithGlobalTag("buildkite.branch", b.Branch),
			tracer.WithGlobalTag("buildkite.job_id", b.JobID),
			tracer.WithGlobalTag("buildkite.job_url", jobURL),
			tracer.WithGlobalTag("buildkite.build_id", buildID),
			tracer.WithGlobalTag("buildkite.build_number", buildNumber),
			tracer.WithGlobalTag("buildkite.build_url", buildURL),
			tracer.WithGlobalTag("buildkite.source", source),
			tracer.WithGlobalTag("buildkite.retry", retry),
			tracer.WithGlobalTag("buildkite.parallel", parallel),
			tracer.WithGlobalTag("buildkite.rebuilt_from_id", rebuiltFromID),
			tracer.WithGlobalTag("buildkite.triggered_from_id", triggeredFromID),
			tracer.WithGlobalTag(ddext.SamplingPriority, ddext.PriorityUserKeep),
		)
		stopper = tracer.Stop
	default:
		b.shell.Commentf("An invalid tracing backend was given: %s. Tracing will not occur.", b.Config.TracingBackend)
		fallthrough
	case "":
		t = opentracing.NoopTracer{}
		stopper = func() {}
	}
	opentracing.SetGlobalTracer(t)

	wireContext := b.extractTraceCtx()

	resourceName := b.OrganizationSlug + "/" + b.PipelineSlug + "/" + label
	span := opentracing.StartSpan(
		"job.run",
		// The span setup code will properly handle this if it's nil
		opentracing.ChildOf(wireContext),
	)

	ctx = opentracing.ContextWithSpan(ctx, span)

	// Some tracer-specific span code.
	if b.Config.TracingBackend == "datadog" {
		// Datadog uses 'resource' instead of opentracing's 'component'. And it's not
		// smart enough to automatically remap component tags so we have to be
		// different here.
		span.SetTag(ddext.ResourceName, resourceName)
		span.SetTag(ddext.AnalyticsEvent, true)
	} else {
		ext.Component.Set(span, resourceName)
	}

	return span, ctx, stopper
}

// executeHook runs a hook script with the hookRunner
func (b *Bootstrap) executeHook(ctx context.Context, scope string, name string, hookPath string, extraEnviron *env.Environment) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "hook.execute")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()
	span.SetTag("hook.type", scope)
	span.SetTag("hook.name", name)
	span.SetTag("hook.command", hookPath)

	name = scope + " " + name

	if !utils.FileExists(hookPath) {
		if b.Debug {
			b.shell.Commentf("Skipping %s hook, no script at \"%s\"", name, hookPath)
		}
		return nil
	}

	b.shell.Headerf("Running %s hook", name)

	if redactor := b.setupRedactor(); redactor != nil {
		defer redactor.Flush()
	}

	// We need a script to wrap the hook script so that we can snaffle the changed
	// environment variables
	script, err := hook.CreateScriptWrapper(hookPath)
	if err != nil {
		b.shell.Errorf("Error creating hook script: %v", err)
		return err
	}
	defer script.Close()

	cleanHookPath := hookPath

	// Show a relative path if we can
	if strings.HasPrefix(hookPath, b.shell.Getwd()) {
		var err error
		if cleanHookPath, err = filepath.Rel(b.shell.Getwd(), hookPath); err != nil {
			cleanHookPath = hookPath
		}
	}

	// Show the hook runner in debug, but the thing being run otherwise 💅🏻
	if b.Debug {
		b.shell.Commentf("A hook runner was written to \"%s\" with the following:", script.Path())
		b.shell.Promptf("%s", process.FormatCommand(script.Path(), nil))
	} else {
		b.shell.Promptf("%s", process.FormatCommand(cleanHookPath, []string{}))
	}

	// Run the wrapper script
	if err = b.shell.RunScript(ctx, script.Path(), extraEnviron); err != nil {
		exitCode := shell.GetExitCode(err)
		b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", exitCode))

		// Give a simpler error if it's just a shell exit error
		if shell.IsExitError(err) {
			return &shell.ExitError{
				Code:    exitCode,
				Message: fmt.Sprintf("The %s hook exited with status %d", name, exitCode),
			}
		}
		return err
	}

	// Store the last hook exit code for subsequent steps
	b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", "0")

	// Get changed environment
	changes, err := script.Changes()
	if err != nil {
		return errors.Wrapf(err, "Failed to get environment")
	}

	// Finally, apply changes to the current shell and config
	b.applyEnvironmentChanges(changes.Env, changes.Dir)
	return nil
}

func (b *Bootstrap) applyEnvironmentChanges(environ *env.Environment, dir string) {
	if dir != b.shell.Getwd() {
		_ = b.shell.Chdir(dir)
	}

	// Do we even have any environment variables to change?
	if environ == nil || environ.Length() == 0 {
		return
	}

	// First, let see any of the environment variables are supposed
	// to change the bootstrap configuration at run time.
	bootstrapConfigEnvChanges := b.Config.ReadFromEnvironment(environ)

	// Print out the env vars that changed. As we go through each
	// one, we'll determine if it was a special "bootstrap"
	// environment variable that has changed the bootstrap
	// configuration at runtime.
	//
	// If it's "special", we'll show the value it was changed to -
	// otherwise we'll hide it. Since we don't know if an
	// environment variable contains sensitive information (i.e.
	// THIRD_PARTY_API_KEY) we'll just not show any values for
	// anything not controlled by us.
	for k, v := range environ.ToMap() {
		if _, ok := bootstrapConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s changed", k)
		}
	}

	// Now that we've finished telling the user what's changed,
	// let's mutate the current shell environment to include all
	// the new values.
	b.shell.Env = b.shell.Env.Merge(environ)
}

func (b *Bootstrap) hasGlobalHook(name string) bool {
	_, err := b.globalHookPath(name)
	return err == nil
}

// Returns the absolute path to a global hook, or os.ErrNotExist if none is found
func (b *Bootstrap) globalHookPath(name string) (string, error) {
	return hook.Find(b.HooksPath, name)
}

// Executes a global hook if one exists
func (b *Bootstrap) executeGlobalHook(ctx context.Context, name string) error {
	if !b.hasGlobalHook(name) {
		return nil
	}
	p, err := b.globalHookPath(name)
	if err != nil {
		return err
	}
	return b.executeHook(ctx, "global", name, p, nil)
}

// Returns the absolute path to a local hook, or os.ErrNotExist if none is found
func (b *Bootstrap) localHookPath(name string) (string, error) {
	dir := filepath.Join(b.shell.Getwd(), ".buildkite", "hooks")
	return hook.Find(dir, name)
}

func (b *Bootstrap) hasLocalHook(name string) bool {
	_, err := b.localHookPath(name)
	return err == nil
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(ctx context.Context, name string) error {
	if !b.hasLocalHook(name) {
		return nil
	}

	localHookPath, err := b.localHookPath(name)
	if err != nil {
		return nil
	}

	// For high-security configs, we allow the disabling of local hooks.
	localHooksEnabled := b.Config.LocalHooksEnabled

	// Allow hooks to disable local hooks by setting BUILDKITE_NO_LOCAL_HOOKS=true
	noLocalHooks, _ := b.shell.Env.Get(`BUILDKITE_NO_LOCAL_HOOKS`)
	if noLocalHooks == "true" || noLocalHooks == "1" {
		localHooksEnabled = false
	}

	if !localHooksEnabled {
		return fmt.Errorf("Refusing to run %s, local hooks are disabled", localHookPath)
	}

	return b.executeHook(ctx, "local", name, localHookPath, nil)
}

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

func dirForRepository(repository string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(repository, "-")
}

// Given a repository, it will add the host to the set of SSH known_hosts on the machine
func addRepositoryHostToSSHKnownHosts(sh *shell.Shell, repository string) {
	if utils.FileExists(repository) {
		return
	}

	knownHosts, err := findKnownHosts(sh)
	if err != nil {
		sh.Warningf("Failed to find SSH known_hosts file: %v", err)
		return
	}

	if err = knownHosts.AddFromRepository(repository); err != nil {
		sh.Warningf("Error adding to known_hosts: %v", err)
		return
	}
}

// setUp is run before all the phases run. It's responsible for initializing the
// bootstrap environment
func (b *Bootstrap) setUp(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "environment")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	// Create an empty env for us to keep track of our env changes in
	b.shell.Env = env.FromSlice(os.Environ())

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if b.BinPath != "" {
		path, _ := b.shell.Env.Get("PATH")
		b.shell.Env.Set("PATH", fmt.Sprintf("%s%s%s", b.BinPath, string(os.PathListSeparator), path))
	}

	// Set a BUILDKITE_BUILD_CHECKOUT_PATH unless one exists already. We do this here
	// so that the environment will have a checkout path to work with
	if _, exists := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"); !exists {
		if b.BuildPath == "" {
			return fmt.Errorf("Must set either a BUILDKITE_BUILD_PATH or a BUILDKITE_BUILD_CHECKOUT_PATH")
		}
		b.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH",
			filepath.Join(b.BuildPath, dirForAgentName(b.AgentName), b.OrganizationSlug, b.PipelineSlug))
	}

	// The job runner sets BUILDKITE_IGNORED_ENV with any keys that were ignored
	// or overwritten. This shows a warning to the user so they don't get confused
	// when their environment changes don't seem to do anything
	if ignored, exists := b.shell.Env.Get("BUILDKITE_IGNORED_ENV"); exists {
		b.shell.Headerf("Detected protected environment variables")
		b.shell.Commentf("Your pipeline environment has protected environment variables set. " +
			"These can only be set via hooks, plugins or the agent configuration.")

		for _, env := range strings.Split(ignored, ",") {
			b.shell.Warningf("Ignored %s", env)
		}

		b.shell.Printf("^^^ +++")
	}

	if b.Debug {
		b.shell.Headerf("Buildkite environment variables")
		for _, e := range b.shell.Env.ToSlice() {
			if strings.HasPrefix(e, "BUILDKITE_AGENT_ACCESS_TOKEN=") {
				b.shell.Printf("BUILDKITE_AGENT_ACCESS_TOKEN=******************")
			} else if strings.HasPrefix(e, "BUILDKITE") || strings.HasPrefix(e, "CI") || strings.HasPrefix(e, "PATH") {
				b.shell.Printf("%s", strings.Replace(e, "\n", "\\n", -1))
			}
		}
	}

	// Disable any interactive Git/SSH prompting
	b.shell.Env.Set("GIT_TERMINAL_PROMPT", "0")

	// It's important to do this before checking out plugins, in case you want
	// to use the global environment hook to whitelist the plugins that are
	// allowed to be used.
	err = b.executeGlobalHook(ctx, "environment")
	return err
}

// tearDown is called before the bootstrap exits, even on error
func (b *Bootstrap) tearDown(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-exit")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	if err = b.executeGlobalHook(ctx, "pre-exit"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "pre-exit"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "pre-exit", b.pluginCheckouts); err != nil {
		return err
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(b.shell) {
		return tearDownDeprecatedDockerIntegration(b.shell)
	}

	for _, dir := range b.cleanupDirs {
		if err = os.RemoveAll(dir); err != nil {
			b.shell.Warningf("Failed to remove dir %s: %v", dir, err)
		}
	}

	return nil
}

func (b *Bootstrap) hasPlugins() bool {
	if b.Config.Plugins == "" {
		return false
	}

	return true
}

func (b *Bootstrap) preparePlugins() error {
	if !b.hasPlugins() {
		return nil
	}

	b.shell.Headerf("Preparing plugins")

	if b.Debug {
		b.shell.Commentf("Plugin JSON is %s", b.Plugins)
	}

	// Check if we can run plugins (disabled via --no-plugins)
	if !b.Config.PluginsEnabled {
		if !b.Config.LocalHooksEnabled {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-local-hooks`")
		} else if !b.Config.CommandEval {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-command-eval`")
		} else {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-plugins`")
		}
	}

	var err error
	b.plugins, err = plugin.CreateFromJSON(b.Config.Plugins)
	if err != nil {
		return errors.Wrap(err, "Failed to parse a plugin definition")
	}

	if b.Debug {
		b.shell.Commentf("Parsed %d plugins", len(b.plugins))
	}

	return nil
}

func (b *Bootstrap) validatePluginCheckout(checkout *pluginCheckout) error {
	if !b.Config.PluginValidation {
		return nil
	}

	if checkout.Definition == nil {
		if b.Debug {
			b.shell.Commentf("Parsing plugin definition for %s from %s", checkout.Plugin.Name(), checkout.CheckoutDir)
		}

		// parse the plugin definition from the plugin checkout dir
		var err error
		checkout.Definition, err = plugin.LoadDefinitionFromDir(checkout.CheckoutDir)

		if err == plugin.ErrDefinitionNotFound {
			b.shell.Warningf("Failed to find plugin definition for plugin %s", checkout.Plugin.Name())
			return nil
		} else if err != nil {
			return err
		}
	}

	val := &plugin.Validator{}
	result := val.Validate(checkout.Definition, checkout.Plugin.Configuration)

	if !result.Valid() {
		b.shell.Headerf("Plugin validation failed for %q", checkout.Plugin.Name())
		json, _ := json.Marshal(checkout.Plugin.Configuration)
		b.shell.Commentf("Plugin configuration JSON is %s", json)
		return result
	}

	b.shell.Commentf("Valid plugin configuration for %q", checkout.Plugin.Name())
	return nil
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (b *Bootstrap) PluginPhase(ctx context.Context) error {
	if len(b.plugins) == 0 {
		if b.Debug {
			b.shell.Commentf("Skipping plugin phase")
		}
		return nil
	}

	checkouts := []*pluginCheckout{}

	// Checkout and validate plugins that aren't vendored
	for _, p := range b.plugins {
		if p.Vendored {
			if b.Debug {
				b.shell.Commentf("Skipping vendored plugin %s", p.Name())
			}
			continue
		}

		checkout, err := b.checkoutPlugin(p)
		if err != nil {
			return errors.Wrapf(err, "Failed to checkout plugin %s", p.Name())
		}

		err = b.validatePluginCheckout(checkout)
		if err != nil {
			return err
		}

		checkouts = append(checkouts, checkout)
	}

	// Store the checkouts for future use
	b.pluginCheckouts = checkouts

	// Now we can run plugin environment hooks too
	return b.executePluginHook(ctx, "environment", checkouts)
}

// VendoredPluginPhase is where plugins that are included in the
// checked out code are added
func (b *Bootstrap) VendoredPluginPhase(ctx context.Context) error {
	if !b.hasPlugins() {
		return nil
	}

	vendoredCheckouts := []*pluginCheckout{}

	// Validate vendored plugins
	for _, p := range b.plugins {
		if !p.Vendored {
			continue
		}

		checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

		pluginLocation, err := filepath.Abs(filepath.Join(checkoutPath, p.Location))
		if err != nil {
			return errors.Wrapf(err, "Failed to resolve vendored plugin path for plugin %s", p.Name())
		}

		if !utils.FileExists(pluginLocation) {
			return fmt.Errorf("Vendored plugin path %s doesn't exist", p.Location)
		}

		checkout := &pluginCheckout{
			Plugin:      p,
			CheckoutDir: pluginLocation,
			HooksDir:    filepath.Join(pluginLocation, "hooks"),
		}

		// Also make sure that plugin is within this repository
		// checkout and isn't elsewhere on the system.
		if !strings.HasPrefix(pluginLocation, checkoutPath+string(os.PathSeparator)) {
			return fmt.Errorf("Vendored plugin paths must be within the checked-out repository")
		}

		err = b.validatePluginCheckout(checkout)
		if err != nil {
			return err
		}

		vendoredCheckouts = append(vendoredCheckouts, checkout)
	}

	// Finally append our vendored checkouts to the rest for subsequent hooks
	b.pluginCheckouts = append(b.pluginCheckouts, vendoredCheckouts...)

	// Now we can run plugin environment hooks too
	return b.executePluginHook(ctx, "environment", vendoredCheckouts)
}

// Executes a named hook on plugins that have it
func (b *Bootstrap) executePluginHook(ctx context.Context, name string, checkouts []*pluginCheckout) error {
	for _, p := range checkouts {
		hookPath, err := hook.Find(p.HooksDir, name)
		// os.IsNotExist() doesn't unwrap wrapped errors (as at Go 1.13).
		// agent is still go pre-1.13 compatible (I think) so we're avoiding errors.Is().
		// In future somebody should check if os.IsNotExist() has added support for
		// error unwrapping, or change this code to errors.Is(err, os.ErrNotExist)
		if os.IsNotExist(err) {
			continue // this plugin does not implement this hook
		} else if err != nil {
			return err
		}

		env, _ := p.ConfigurationToEnvironment()
		if err := b.executeHook(ctx, "plugin", p.Plugin.Name()+" "+name, hookPath, env); err != nil {
			return err
		}
	}

	return nil
}

// If any plugin has a hook by this name
func (b *Bootstrap) hasPluginHook(name string) bool {
	for _, p := range b.pluginCheckouts {
		if _, err := hook.Find(p.HooksDir, name); err == nil {
			return true
		}
	}
	return false
}

// Checkout a given plugin to the plugins directory and return that directory
func (b *Bootstrap) checkoutPlugin(p *plugin.Plugin) (*pluginCheckout, error) {
	// Make sure we have a plugin path before trying to do anything
	if b.PluginsPath == "" {
		return nil, fmt.Errorf("Can't checkout plugin without a `plugins-path`")
	}

	// Get the identifer for the plugin
	id, err := p.Identifier()
	if err != nil {
		return nil, err
	}

	// Ensure the plugin directory exists, otherwise we can't create the lock
	err = os.MkdirAll(b.PluginsPath, 0777)
	if err != nil {
		return nil, err
	}

	// Try and lock this particular plugin while we check it out (we create
	// the file outside of the plugin directory so git clone doesn't have
	// a cry about the directory not being empty)
	pluginCheckoutHook, err := b.shell.LockFile(filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
	if err != nil {
		return nil, err
	}
	defer pluginCheckoutHook.Unlock()

	// Create a path to the plugin
	directory := filepath.Join(b.PluginsPath, id)
	pluginGitDirectory := filepath.Join(directory, ".git")
	checkout := &pluginCheckout{
		Plugin:      p,
		CheckoutDir: directory,
		HooksDir:    filepath.Join(directory, "hooks"),
	}

	// Has it already been checked out?
	if utils.FileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(b.shell, directory, "--short=7", "HEAD")
		if err != nil {
			b.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			b.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		return checkout, nil
	}

	// Make the directory
	err = os.MkdirAll(directory, 0777)
	if err != nil {
		return nil, err
	}

	// Once we've got the lock, we need to make sure another process didn't already
	// checkout the plugin
	if utils.FileExists(pluginGitDirectory) {
		b.shell.Commentf("Plugin \"%s\" already checked out", p.Label())
		return checkout, nil
	}

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	b.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, directory)

	if b.Debug {
		b.shell.Commentf("Checking if \"%s\" is a local repository", repo)
	}

	// Switch to the plugin directory
	previousWd := b.shell.Getwd()
	if err = b.shell.Chdir(directory); err != nil {
		return nil, err
	}

	// Switch back to the previous working directory
	defer b.shell.Chdir(previousWd)

	b.shell.Commentf("Switching to the plugin directory")

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(b.shell, repo)
	}

	// Plugin clones shouldn't use custom GitCloneFlags
	if err = b.shell.Run("git", "clone", "-v", "--", repo, "."); err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		b.shell.Commentf("Checking out `%s`", p.Version)
		if err = b.shell.Run("git", "checkout", "-f", p.Version); err != nil {
			return nil, err
		}
	}

	return checkout, nil
}

func (b *Bootstrap) removeCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// on windows, sometimes removing large dirs can fail for various reasons
	// for instance having files open
	// see https://github.com/golang/go/issues/20841
	for i := 0; i < 10; i++ {
		b.shell.Commentf("Removing %s", checkoutPath)
		if err := os.RemoveAll(checkoutPath); err != nil {
			b.shell.Errorf("Failed to remove \"%s\" (%s)", checkoutPath, err)
		} else {
			if _, err := os.Stat(checkoutPath); os.IsNotExist(err) {
				return nil
			} else {
				b.shell.Errorf("Failed to remove %s", checkoutPath)
			}
		}
		b.shell.Commentf("Waiting 10 seconds")
		<-time.After(time.Second * 10)
	}

	return fmt.Errorf("Failed to remove %s", checkoutPath)
}

func (b *Bootstrap) createCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !utils.FileExists(checkoutPath) {
		b.shell.Commentf("Creating \"%s\"", checkoutPath)
		if err := os.MkdirAll(checkoutPath, 0777); err != nil {
			return err
		}
	}

	if b.shell.Getwd() != checkoutPath {
		if err := b.shell.Chdir(checkoutPath); err != nil {
			return err
		}
	}

	return nil
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (b *Bootstrap) CheckoutPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	if err = b.executeGlobalHook(ctx, "pre-checkout"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "pre-checkout", b.pluginCheckouts); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		b.shell.Headerf("Cleaning pipeline checkout")
		if err = b.removeCheckoutDir(); err != nil {
			return err
		}
	}

	b.shell.Headerf("Preparing working directory")

	// If we have a blank repository then use a temp dir for builds
	if b.Config.Repository == "" {
		var buildDir string
		buildDir, err = ioutil.TempDir("", "buildkite-job-"+b.Config.JobID)
		if err != nil {
			return err
		}
		b.shell.Env.Set(`BUILDKITE_BUILD_CHECKOUT_PATH`, buildDir)

		// Track the directory so we can remove it at the end of the bootstrap
		b.cleanupDirs = append(b.cleanupDirs, buildDir)
	}

	// Make sure the build directory exists
	if err = b.createCheckoutDir(); err != nil {
		return err
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case b.hasPluginHook("checkout"):
		if err = b.executePluginHook(ctx, "checkout", b.pluginCheckouts); err != nil {
			return err
		}
	case b.hasGlobalHook("checkout"):
		if err = b.executeGlobalHook(ctx, "checkout"); err != nil {
			return err
		}
	default:
		if b.Config.Repository != "" {
			err = retry.Do(func(s *retry.Stats) error {
				err := b.defaultCheckoutPhase()
				if err == nil {
					return nil
				}

				switch {
				case shell.IsExitError(err) && shell.GetExitCode(err) == -1:
					b.shell.Warningf("Checkout was interrupted by a signal")
					s.Break()

				case errors.Cause(err) == context.Canceled:
					b.shell.Warningf("Checkout was cancelled")
					s.Break()

				default:
					b.shell.Warningf("Checkout failed! %s (%s)", err, s)

					// Specifically handle git errors
					if ge, ok := err.(*gitError); ok {
						switch ge.Type {
						// These types can fail because of corrupted checkouts
						case gitErrorClone:
						case gitErrorClean:
						case gitErrorCleanSubmodules:
							// do nothing, this will fall through to destroy the checkout

						default:
							return err
						}
					}

					// Checkout can fail because of corrupted files in the checkout
					// which can leave the agent in a state where it keeps failing
					// This removes the checkout dir, which means the next checkout
					// will be a lot slower (clone vs fetch), but hopefully will
					// allow the agent to self-heal
					_ = b.removeCheckoutDir()

					// Now make sure the build directory exists again before we try
					// to checkout again, or proceed and run hooks which presume the
					// checkout dir exists
					if err := b.createCheckoutDir(); err != nil {
						return err
					}

				}

				return err
			}, &retry.Config{Maximum: 3, Interval: 2 * time.Second})
			if err != nil {
				return err
			}
		} else {
			b.shell.Commentf("Skipping checkout, BUILDKITE_REPO is empty")
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run post-checkout hooks
	if err = b.executeGlobalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "post-checkout", b.pluginCheckouts); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		b.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err = b.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

func hasGitSubmodules(sh *shell.Shell) bool {
	return utils.FileExists(filepath.Join(sh.Getwd(), ".gitmodules"))
}

func hasGitCommit(sh *shell.Shell, gitDir string, commit string) bool {
	// Resolve commit to an actual commit object
	output, err := sh.RunAndCapture("git", "--git-dir", gitDir, "rev-parse", commit+"^{commit}")
	if err != nil {
		return false
	}

	// Filter out commitish things like HEAD et al
	if strings.TrimSpace(output) != commit {
		return false
	}

	// Otherwise it's a commit in the repo
	return true
}

func (b *Bootstrap) updateGitMirror() (string, error) {
	// Create a unique directory for the repository mirror
	mirrorDir := filepath.Join(b.Config.GitMirrorsPath, dirForRepository(b.Repository))

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !utils.FileExists(baseDir) {
		b.shell.Commentf("Creating \"%s\"", baseDir)
		if err := os.MkdirAll(baseDir, 0777); err != nil {
			return "", err
		}
	}

	b.shell.Chdir(b.Config.GitMirrorsPath)

	lockTimeout := time.Second * time.Duration(b.GitMirrorsLockTimeout)

	if b.Debug {
		b.shell.Commentf("Acquiring mirror repository clone lock")
	}

	// Lock the mirror dir to prevent concurrent clones
	mirrorCloneLock, err := b.shell.LockFile(mirrorDir+".clonelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorCloneLock.Unlock()

	// If we don't have a mirror, we need to clone it
	if !utils.FileExists(mirrorDir) {
		b.shell.Commentf("Cloning a mirror of the repository to %q", mirrorDir)
		flags := "--mirror " + b.GitCloneMirrorFlags
		if err := gitClone(b.shell, flags, b.Repository, mirrorDir); err != nil {
			return "", err
		}

		return mirrorDir, nil
	}

	// If it exists, immediately release the clone lock
	mirrorCloneLock.Unlock()

	// Check if the mirror has a commit, this is atomic so should be safe to do
	if hasGitCommit(b.shell, mirrorDir, b.Commit) {
		b.shell.Commentf("Commit %q exists in mirror", b.Commit)
		return mirrorDir, nil
	}

	if b.Debug {
		b.shell.Commentf("Acquiring mirror repository update lock")
	}

	// Lock the mirror dir to prevent concurrent updates
	mirrorUpdateLock, err := b.shell.LockFile(mirrorDir+".updatelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorUpdateLock.Unlock()

	// Check again after we get a lock, in case the other process has already updated
	if hasGitCommit(b.shell, mirrorDir, b.Commit) {
		b.shell.Commentf("Commit %q exists in mirror", b.Commit)
		return mirrorDir, nil
	}

	b.shell.Commentf("Updating existing repository mirror to find commit %s", b.Commit)

	// Update the the origin of the repository so we can gracefully handle repository renames
	if err := b.shell.Run("git", "--git-dir", mirrorDir, "remote", "set-url", "origin", b.Repository); err != nil {
		return "", err
	}

	if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and mirror pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)
		// Fetch the PR head from the upstream repository into the mirror.
		if err := b.shell.Run("git", "--git-dir", mirrorDir, "fetch", "origin", refspec); err != nil {
			return "", err
		}
	} else {
		// Fetch the build branch from the upstream repository into the mirror.
		if err := b.shell.Run("git", "--git-dir", mirrorDir, "fetch", "origin", b.Branch); err != nil {
			return "", err
		}
	}

	return mirrorDir, nil
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (b *Bootstrap) defaultCheckoutPhase() error {
	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(b.shell, b.Repository)
	}

	var mirrorDir string

	// If we can, get a mirror of the git repository to use for reference later
	if experiments.IsEnabled(`git-mirrors`) && b.Config.GitMirrorsPath != "" && b.Config.Repository != "" {
		b.shell.Commentf("Using git-mirrors experiment 🧪")
		var err error
		mirrorDir, err = b.updateGitMirror()
		if err != nil {
			return err
		}
		b.shell.Env.Set("BUILDKITE_REPO_MIRROR", mirrorDir)
	}

	// Make sure the build directory exists and that we change directory into it
	if err := b.createCheckoutDir(); err != nil {
		return err
	}

	gitCloneFlags := b.GitCloneFlags
	if mirrorDir != "" {
		gitCloneFlags += fmt.Sprintf(" --reference %q", mirrorDir)
	}

	// Does the git directory exist?
	existingGitDir := filepath.Join(b.shell.Getwd(), ".git")
	if utils.FileExists(existingGitDir) {
		// Update the the origin of the repository so we can gracefully handle repository renames
		if err := b.shell.Run("git", "remote", "set-url", "origin", b.Repository); err != nil {
			return err
		}
	} else {
		if err := gitClone(b.shell, gitCloneFlags, b.Repository, "."); err != nil {
			return err
		}
	}

	// Git clean prior to checkout, we do this even if submodules have been
	// disabled to ensure previous submodules are cleaned up
	if hasGitSubmodules(b.shell) {
		if err := gitCleanSubmodules(b.shell, b.GitCleanFlags); err != nil {
			return err
		}
	}

	if err := gitClean(b.shell, b.GitCleanFlags); err != nil {
		return err
	}

	gitFetchFlags := b.GitFetchFlags

	// If a refspec is provided then use it instead.
	// i.e. `refs/not/a/head`
	if b.RefSpec != "" {
		b.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(b.shell, gitFetchFlags, "origin", b.RefSpec); err != nil {
			return err
		}

		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not there is a current head in this repository or another which
		// references the commit. We presume a commit sha is provided. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
	} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and checkout pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)

		if err := gitFetch(b.shell, gitFetchFlags, "origin", refspec); err != nil {
			return err
		}

		gitFetchHead, _ := b.shell.RunAndCapture("git", "rev-parse", "FETCH_HEAD")
		b.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
	} else if b.Commit == "HEAD" {
		b.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(b.shell, gitFetchFlags, "origin", b.Branch); err != nil {
			return err
		}

		// Otherwise fetch and checkout the commit directly. Some repositories don't
		// support fetching a specific commit so we fall back to fetching all heads
		// and tags, hoping that the commit is included.
	} else {
		if err := gitFetch(b.shell, gitFetchFlags, "origin", b.Commit); err != nil {
			// By default `git fetch origin` will only fetch tags which are
			// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
			// fetch all tags in addition to the default refspec, but pre 1.9.0 it
			// excludes the default refspec.
			gitFetchRefspec, _ := b.shell.RunAndCapture("git", "config", "remote.origin.fetch")
			if err := gitFetch(b.shell, gitFetchFlags, "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*"); err != nil {
				return err
			}
		}
	}

	if b.Commit == "HEAD" {
		if err := gitCheckout(b.shell, "-f", "FETCH_HEAD"); err != nil {
			return err
		}
	} else {
		if err := gitCheckout(b.shell, "-f", b.Commit); err != nil {
			return err
		}
	}

	var gitSubmodules bool
	if !b.GitSubmodules && hasGitSubmodules(b.shell) {
		b.shell.Warningf("This repository has submodules, but submodules are disabled at an agent level")
	} else if b.GitSubmodules && hasGitSubmodules(b.shell) {
		b.shell.Commentf("Git submodules detected")
		gitSubmodules = true
	}

	if gitSubmodules {
		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the bootstrap
		// script, and show an informative error.
		if err := b.shell.Run("git", "submodule", "sync", "--recursive"); err != nil {
			gitVersionOutput, _ := b.shell.RunAndCapture("git", "--version")
			b.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		// Checking for submodule repositories
		submoduleRepos, err := gitEnumerateSubmoduleURLs(b.shell)
		if err != nil {
			b.shell.Warningf("Failed to enumerate git submodules: %v", err)
		} else {
			for _, repository := range submoduleRepos {
				// submodules might need their fingerprints verified too
				if b.SSHKeyscan {
					addRepositoryHostToSSHKnownHosts(b.shell, repository)
				}
			}
		}

		if err := b.shell.Run("git", "submodule", "update", "--init", "--recursive", "--force"); err != nil {
			return err
		}

		if err := b.shell.Run("git", "submodule", "foreach", "--recursive", "git reset --hard"); err != nil {
			return err
		}
	}

	// Git clean after checkout. We need to do this because submodules could have
	// changed in between the last checkout and this one. A double clean is the only
	// good solution to this problem that we've found
	b.shell.Commentf("Cleaning again to catch any post-checkout changes")

	if err := gitClean(b.shell, b.GitCleanFlags); err != nil {
		return err
	}

	if gitSubmodules {
		if err := gitCleanSubmodules(b.shell, b.GitCleanFlags); err != nil {
			return err
		}
	}

	if _, hasToken := b.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN"); !hasToken {
		b.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// resolve BUILDKITE_COMMIT based on the local git repo
	if experiments.IsEnabled(`resolve-commit-after-checkout`) {
		b.shell.Commentf("Using resolve-commit-after-checkout experiment 🧪")
		b.resolveCommit()
	}

	// Grab author and commit information and send
	// it back to Buildkite. But before we do,
	// we'll check to see if someone else has done
	// it first.
	b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if err := b.shell.Run("buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
		b.shell.Commentf("Sending Git commit information back to Buildkite")
		out, err := b.shell.RunAndCapture("git", "--no-pager", "show", "HEAD", "-s", "--format=fuller", "--no-color", "--")
		if err != nil {
			return err
		}
		stdin := strings.NewReader(out)
		if err := b.shell.WithStdin(stdin).Run("buildkite-agent", "meta-data", "set", "buildkite:git:commit"); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bootstrap) resolveCommit() {
	commitRef, _ := b.shell.Env.Get("BUILDKITE_COMMIT")
	if commitRef == "" {
		b.shell.Warningf("BUILDKITE_COMMIT was empty")
		return
	}
	cmdOut, err := b.shell.RunAndCapture(`git`, `rev-parse`, commitRef)
	if err != nil {
		b.shell.Warningf("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	if trimmedCmdOut != commitRef {
		b.shell.Commentf("Updating BUILDKITE_COMMIT from %q to %q", commitRef, trimmedCmdOut)
		b.shell.Env.Set(`BUILDKITE_COMMIT`, trimmedCmdOut)
	}
}

// runPreCommandHooks runs the pre-command hooks and adds tracing spans.
func (b *Bootstrap) runPreCommandHooks(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-command hooks")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	if err = b.executeGlobalHook(ctx, "pre-command"); err != nil {
		return err
	}
	if err = b.executeLocalHook(ctx, "pre-command"); err != nil {
		return err
	}
	if err = b.executePluginHook(ctx, "pre-command", b.pluginCheckouts); err != nil {
		return err
	}
	return nil
}

// runCommand runs the command and adds tracing spans.
func (b *Bootstrap) runCommand(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "command")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	// There can only be one command hook, so we check them in order of plugin, local
	switch {
	case b.hasPluginHook("command"):
		err = b.executePluginHook(ctx, "command", b.pluginCheckouts)
	case b.hasLocalHook("command"):
		err = b.executeLocalHook(ctx, "command")
	case b.hasGlobalHook("command"):
		err = b.executeGlobalHook(ctx, "command")
	default:
		err = b.defaultCommandPhase(ctx)
	}
	return err
}

// runPostCommandHooks runs the post-command hooks and adds tracing spans.
func (b *Bootstrap) runPostCommandHooks(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "post-command hooks")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	if err = b.executeGlobalHook(ctx, "post-command"); err != nil {
		return err
	}
	if err = b.executeLocalHook(ctx, "post-command"); err != nil {
		return err
	}
	if err = b.executePluginHook(ctx, "post-command", b.pluginCheckouts); err != nil {
		return err
	}
	return nil
}

// CommandPhase determines how to run the build, and then runs it
func (b *Bootstrap) CommandPhase(ctx context.Context) (error, error) {
	// Run pre-command hooks
	if err := b.runPreCommandHooks(ctx); err != nil {
		return err, nil
	}

	// Run the actual command
	commandExitError := b.runCommand(ctx)
	var realCommandError error

	// If the command returned an exit that wasn't a `exec.ExitError`
	// (which is returned when the command is actually run, but fails),
	// then we'll show it in the log.
	if shell.IsExitError(commandExitError) {
		if shell.IsExitSignaled(commandExitError) {
			b.shell.Errorf("The command was interrupted by a signal")
		} else {
			realCommandError = commandExitError
			b.shell.Errorf("The command exited with status %d", shell.GetExitCode(commandExitError))
		}
	} else if commandExitError != nil {
		b.shell.Errorf(commandExitError.Error())
	}

	// Expand the command header if the command fails for any reason
	if commandExitError != nil {
		b.shell.Printf("^^^ +++")
	}

	// Save the command exit status to the env so hooks + plugins can access it. If there is no error
	// this will be zero. It's used to set the exit code later, so it's important
	b.shell.Env.Set("BUILDKITE_COMMAND_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(commandExitError)))

	// Run post-command hooks
	if err := b.runPostCommandHooks(ctx); err != nil {
		return err, realCommandError
	}

	return nil, realCommandError
}

// defaultCommandPhase is executed if there is no global or plugin command hook
func (b *Bootstrap) defaultCommandPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "hook.execute")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()
	span.SetTag("hook.name", "command")
	span.SetTag("hook.type", "default")

	// Make sure we actually have a command to run
	if strings.TrimSpace(b.Command) == "" {
		return fmt.Errorf("No command has been provided")
	}

	scriptFileName := strings.Replace(b.Command, "\n", "", -1)
	pathToCommand, err := filepath.Abs(filepath.Join(b.shell.Getwd(), scriptFileName))
	commandIsScript := err == nil && utils.FileExists(pathToCommand)
	span.SetTag("hook.command", pathToCommand)

	// If the command isn't a script, then it's something we need
	// to eval. But before we even try running it, we should double
	// check that the agent is allowed to eval commands.
	if !commandIsScript && !b.CommandEval {
		b.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	// Also make sure that the script we've resolved is definitely within this
	// repository checkout and isn't elsewhere on the system.
	if commandIsScript && !b.CommandEval && !strings.HasPrefix(pathToCommand, b.shell.Getwd()+string(os.PathSeparator)) {
		b.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is only allowed to run scripts within your repository. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	var cmdToExec string

	// The shell gets parsed based on the operating system
	var shell []string
	shell, err = shellwords.Split(b.Shell)
	if err != nil {
		return fmt.Errorf("Failed to split shell (%q) into tokens: %v", b.Shell, err)
	}

	if len(shell) == 0 {
		return fmt.Errorf("No shell set for bootstrap")
	}

	// Windows CMD.EXE is horrible and can't handle newline delimited commands. We write
	// a batch script so that it works, but we don't like it
	if strings.ToUpper(filepath.Base(shell[0])) == `CMD.EXE` {
		batchScript, err := b.writeBatchScript(b.Command)
		if err != nil {
			return err
		}
		defer os.Remove(batchScript)

		b.shell.Headerf("Running batch script")
		if b.Debug {
			contents, err := ioutil.ReadFile(batchScript)
			if err != nil {
				return err
			}
			b.shell.Commentf("Wrote batch script %s\n%s", batchScript, contents)
		}

		cmdToExec = batchScript
	} else if commandIsScript {
		// If we're running without CommandEval, the usual reason is we're
		// trying to protect the agent from malicious activity from outside
		// (including from the master).
		//
		// Because without this guard, we'll try to make the named file +x,
		// and then attempt to run it, irrespective of any git attributes,
		// should the queue source/master be compromised, this then becomes a
		// vector through which a no-command-eval agent could potentially be
		// made to run code not desired or vetted by the operator.
		//
		// Such undesired payloads could be delivered by hiding that payload in
		// non-executable objects in the repo (such as through partial shell
		// fragments, or other material not intended to be run on its own),
		// or by obfuscating binary executable code into other types of binaries.
		//
		// This also closes the risk factor with agents where you
		// may have a dangerous script committed, but not executable (maybe
		// because it's part of a deployment process), but you don't want that
		// script to ever be executed on the buildkite agent itself!  With
		// command-eval agents, such risks are everpresent since the master
		// can tell the agent to do anything anyway, but no-command-eval agents
		// shouldn't be vulnerable to this!
		if b.Config.CommandEval {
			// Make script executable
			if err = utils.ChmodExecutable(pathToCommand); err != nil {
				b.shell.Warningf("Error marking script %q as executable: %v", pathToCommand, err)
				return err
			}
		}

		// Make the path relative to the shell working dir
		scriptPath, err := filepath.Rel(b.shell.Getwd(), pathToCommand)
		if err != nil {
			return err
		}

		b.shell.Headerf("Running script")
		cmdToExec = fmt.Sprintf(".%c%s", os.PathSeparator, scriptPath)
	} else {
		b.shell.Headerf("Running commands")
		cmdToExec = b.Command
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(b.shell) {
		if b.Debug {
			b.shell.Commentf("Detected deprecated docker environment variables")
		}
		err = runDeprecatedDockerIntegration(b.shell, []string{cmdToExec})
		return err
	}

	// If we aren't running a script, try and detect if we are using a posix shell
	// and if so add a trap so that the intermediate shell doesn't swallow signals
	// from cancellation
	if !commandIsScript && isPosixShell(shell) {
		cmdToExec = fmt.Sprintf(`trap 'kill -- $$' INT TERM QUIT; %s`, cmdToExec)
	}

	if redactor := b.setupRedactor(); redactor != nil {
		defer redactor.Flush()
	}

	var cmd []string
	cmd = append(cmd, shell...)
	cmd = append(cmd, cmdToExec)

	if b.Debug {
		b.shell.Promptf("%s", process.FormatCommand(cmd[0], cmd[1:]))
	} else {
		b.shell.Promptf("%s", cmdToExec)
	}

	err = b.shell.RunWithoutPromptWithContext(ctx, cmd[0], cmd[1:]...)
	return err
}

// isPosixShell attempts to detect posix shells (e.g bash, sh, zsh )
func isPosixShell(shell []string) bool {
	bin := filepath.Base(shell[0])

	if filepath.Base(shell[0]) == `env` {
		bin = filepath.Base(shell[1])
	}

	switch bin {
	case `bash`, `sh`, `zsh`, `ksh`, `dash`:
		return true
	default:
		return false
	}
}

func (b *Bootstrap) writeBatchScript(cmd string) (string, error) {
	scriptFile, err := shell.TempFileWithExtension(
		`buildkite-script.bat`,
	)
	if err != nil {
		return "", err
	}
	defer scriptFile.Close()

	var scriptContents = "@echo off\n"

	for _, line := range strings.Split(cmd, "\n") {
		if line != "" {
			scriptContents += line + "\n" + "if %errorlevel% neq 0 exit /b %errorlevel%\n"
		}
	}

	_, err = io.WriteString(scriptFile, scriptContents)
	if err != nil {
		return "", err
	}

	return scriptFile.Name(), nil

}

func (b *Bootstrap) uploadArtifacts(ctx context.Context) error {
	if b.AutomaticArtifactUploadPaths == "" {
		return nil
	}

	span, ctx := tracetools.StartSpanFromContext(ctx, "upload artifacts")
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	// Run pre-artifact hooks
	if err = b.executeGlobalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "pre-artifact", b.pluginCheckouts); err != nil {
		return err
	}

	// Run the artifact upload command
	b.shell.Headerf("Uploading artifacts")
	args := []string{"artifact", "upload", b.AutomaticArtifactUploadPaths}

	// If blank, the upload destination is buildkite
	if b.ArtifactUploadDestination != "" {
		b.shell.Commentf("Using default artifact upload destination")
		args = append(args, b.ArtifactUploadDestination)
	}

	if err = b.shell.Run("buildkite-agent", args...); err != nil {
		return err
	}

	// Run post-artifact hooks
	if err = b.executeGlobalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "post-artifact", b.pluginCheckouts); err != nil {
		return err
	}

	return nil
}

// Check for ignored env variables from the job runner. Some
// env (e.g BUILDKITE_BUILD_PATH) can only be set from config or by hooks.
// If these env are set at a pipeline level, we rewrite them to BUILDKITE_X_BUILD_PATH
// and warn on them here so that users know what is going on
func (b *Bootstrap) ignoredEnv() []string {
	var ignored []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, `BUILDKITE_X_`) {
			ignored = append(ignored, fmt.Sprintf("BUILDKITE_%s",
				strings.TrimPrefix(env, `BUILDKITE_X_`)))
		}
	}
	return ignored
}

// Check the redaction config and create a redactor if necessary - may return
// nil if there's nothing to redact.
// The redactor is returned so the caller can `defer redactor.Flush()`
func (b *Bootstrap) setupRedactor() *Redactor {
	if experiments.IsEnabled("output-redactor") {
		b.shell.Commentf("Using output-redactor experiment 🧪")
	} else {
		return nil
	}

	valuesToRedact := getValuesToRedact(b.shell, b.Config.RedactedVars, b.shell.Env.ToMap())

	// If the shell Writer is already a Redactor, don't layer another Redactor
	// on top of it
	if redactor, ok := b.shell.Writer.(*Redactor); ok {
		// Still need to reset the values when re-using the Redactor
		redactor.Reset(valuesToRedact)
		return redactor
	}
	if len(valuesToRedact) == 0 {
		return nil
	}
	redactor := NewRedactor(b.shell.Writer, "[REDACTED]", valuesToRedact)
	b.shell.Writer = redactor
	return redactor
}

// Given a redaction config string and an environment map, return the list of values to be redacted.
// Lifted out of Bootstrap.setupRedactor to facilitate testing
func getValuesToRedact(logger shell.Logger, patterns []string, environment map[string]string) []string {
	var valuesToRedact []string

	for varName, varValue := range environment {
		for _, pattern := range patterns {
			matched, err := path.Match(pattern, varName)
			if err != nil {
				// path.ErrBadPattern is the only error returned by path.Match
				logger.Warningf("Bad redacted vars pattern: %s", pattern)
				continue
			}

			if matched {
				if len(varValue) < RedactLengthMin {
					logger.Warningf("Value of %s below minimum length and will not be redacted", varName)
				} else {
					valuesToRedact = append(valuesToRedact, varValue)
				}
				break // Break pattern loop, continue to next env var
			}
		}
	}

	return valuesToRedact
}

type pluginCheckout struct {
	*plugin.Plugin
	*plugin.Definition
	CheckoutDir string
	HooksDir    string
}
