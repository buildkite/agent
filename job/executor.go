// package job provides management of the phases of execution of a
// Buildkite job.
//
// It is intended for internal use by buildkite-agent only.
package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/job/shell"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/redaction"
	"github.com/buildkite/agent/v3/shellscript"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/utils"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

// Executor represents the phases of execution in a Buildkite Job. It's run as
// a sub-process of the buildkite-agent and finishes at the conclusion of a job.
//
// Historically (prior to v3) the executor was a shell script, but was ported
// to Go for portability and testability.
// It also used to be called a "bootstrap", so  you might see that verbiage hanging around in some places.
type Executor struct {
	// Config provides the executor configuration
	Config

	// Shell is the shell environment for the executor
	shell *shell.Shell

	// Plugins to use
	plugins []*plugin.Plugin

	// Plugin checkouts from the plugin phases
	pluginCheckouts []*pluginCheckout

	// Directories to clean up at end of executor
	cleanupDirs []string

	// A channel to track cancellation
	cancelCh chan struct{}
}

// NewExecutor returns a new Executor instance
func NewExecutor(conf Config) *Executor {
	return &Executor{
		Config:   conf,
		cancelCh: make(chan struct{}),
	}
}

// Run the executor and return the exit code
func (b *Executor) Run(ctx context.Context) (exitCode int) {
	// Check if not nil to allow for tests to overwrite shell
	if b.shell == nil {
		var err error
		b.shell, err = shell.New()
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			return 1
		}

		b.shell.PTY = b.Config.RunInPty
		b.shell.Debug = b.Config.Debug
		b.shell.InterruptSignal = b.Config.CancelSignal
	}
	if experiments.IsEnabled(experiments.KubernetesExec) {
		kubernetesClient := &kubernetes.Client{}
		if err := b.startKubernetesClient(ctx, kubernetesClient); err != nil {
			b.shell.Errorf("Failed to start kubernetes client: %v", err)
			return 1
		}
		defer func() {
			kubernetesClient.Exit(exitCode)
		}()
	}

	var err error
	span, ctx, stopper := b.startTracing(ctx)
	defer stopper()
	defer func() { span.FinishWithError(err) }()

	// Create a context to use for cancelation of the job
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Listen for cancellation
	go func() {
		select {
		case <-ctx.Done():
			return

		case <-b.cancelCh:
			b.shell.Commentf("Received cancellation signal, interrupting")
			b.shell.Interrupt()
			cancel()
		}
	}()

	// Create an empty env for us to keep track of our env changes in
	b.shell.Env = env.FromSlice(os.Environ())

	// Initialize the job API, iff the experiment is enabled. Noop otherwise
	cleanup, err := b.startJobAPI()
	if err != nil {
		b.shell.Errorf("Error setting up job API: %v", err)
		return 1
	}

	defer cleanup()

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		if err = b.tearDown(ctx); err != nil {
			b.shell.Errorf("Error tearing down executor: %v", err)

			// this gets passed back via the named return
			exitCode = shell.GetExitCode(err)
		}
	}()

	// Initialize the environment, a failure here will still call the tearDown
	if err = b.setUp(ctx); err != nil {
		b.shell.Errorf("Error setting up executor: %v", err)
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

	// Execute the executor phases in order
	var phaseErr error

	if includePhase("plugin") {
		phaseErr = b.preparePlugins()

		if phaseErr == nil {
			phaseErr = b.PluginPhase(ctx)
		}
	}

	if phaseErr == nil && includePhase("checkout") {
		phaseErr = b.CheckoutPhase(cancelCtx)
	} else {
		checkoutDir, exists := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
		if exists {
			_ = b.shell.Chdir(checkoutDir)
		}
	}

	if phaseErr == nil && includePhase("plugin") {
		phaseErr = b.VendoredPluginPhase(ctx)
	}

	if phaseErr == nil && includePhase("command") {
		var commandErr error
		phaseErr, commandErr = b.CommandPhase(ctx)
		/*
			Five possible states at this point:

			Pre-command failed
			Pre-command succeeded, command failed, post-command succeeded
			Pre-command succeeded, command failed, post-command failed
			Pre-command succeeded, command succeeded, post-command succeeded
			Pre-command succeeded, command succeeded, post-command failed

			All states should attempt an artifact upload, to change this would
			not be backwards compatible.

			At this point, if commandErr != nil, BUILDKITE_COMMAND_EXIT_STATUS
			has been set.
		*/

		// Add command exit error info. This is distinct from a phaseErr, which is
		// an error from the hook/job logic. These are both good to report but
		// shouldn't override each other in reporting.
		if commandErr != nil {
			b.shell.Printf("user command error: %v", commandErr)
			span.RecordError(commandErr)
		}

		// Only upload artifacts as part of the command phase
		if err = b.artifactPhase(ctx); err != nil {
			b.shell.Errorf("%v", err)

			if commandErr != nil {
				// Both command, and upload have errored.
				//
				// Ignore the agent upload error, rely on the phase and command
				// error reporting below.
			} else {
				// Only upload has errored, report its error.
				return shell.GetExitCode(err)
			}
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
	exitStatus, _ := b.shell.Env.Get("BUILDKITE_COMMAND_EXIT_STATUS")
	exitStatusCode, _ := strconv.Atoi(exitStatus)

	return exitStatusCode
}

// Cancel interrupts any running shell processes and causes the executor to stop
func (b *Executor) Cancel() error {
	b.cancelCh <- struct{}{}
	return nil
}

type HookConfig struct {
	Name           string
	Scope          string
	Path           string
	Env            *env.Environment
	SpanAttributes map[string]string
	PluginName     string
}

func (b *Executor) tracingImplementationSpecificHookScope(scope string) string {
	if b.TracingBackend != tracetools.BackendOpenTelemetry {
		return scope
	}

	// The scope names local and global are confusing, and different to what we document, so we should use the
	// documented names (repository and agent, respectively) in OpenTelemetry.
	// However, we need to keep the OpenTracing/Datadog implementation the same, hence this horrible function
	switch scope {
	case "local":
		return "repository"
	case "global":
		return "agent"
	default:
		return scope
	}
}

// executeHook runs a hook script with the hookRunner
func (b *Executor) executeHook(ctx context.Context, hookCfg HookConfig) error {
	scopeName := b.tracingImplementationSpecificHookScope(hookCfg.Scope)
	spanName := b.implementationSpecificSpanName(fmt.Sprintf("%s %s hook", scopeName, hookCfg.Name), "hook.execute")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()
	span.AddAttributes(map[string]string{
		"hook.type":    scopeName,
		"hook.name":    hookCfg.Name,
		"hook.command": hookCfg.Path,
	})
	span.AddAttributes(hookCfg.SpanAttributes)

	hookName := hookCfg.Scope
	if hookCfg.PluginName != "" {
		hookName += " " + hookCfg.PluginName
	}
	hookName += " " + hookCfg.Name

	if !utils.FileExists(hookCfg.Path) {
		if b.Debug {
			b.shell.Commentf("Skipping %s hook, no script at \"%s\"", hookName, hookCfg.Path)
		}
		return nil
	}

	b.shell.Headerf("Running %s hook", hookName)

	redactors := b.setupRedactors()
	defer redactors.Flush()

	// We need a script to wrap the hook script so that we can snaffle the changed
	// environment variables
	script, err := hook.NewScriptWrapper(hook.WithHookPath(hookCfg.Path))
	if err != nil {
		b.shell.Errorf("Error creating hook script: %v", err)
		return err
	}
	defer script.Close()

	cleanHookPath := hookCfg.Path

	// Show a relative path if we can
	if strings.HasPrefix(hookCfg.Path, b.shell.Getwd()) {
		var err error
		if cleanHookPath, err = filepath.Rel(b.shell.Getwd(), hookCfg.Path); err != nil {
			cleanHookPath = hookCfg.Path
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
	if err = b.shell.RunScript(ctx, script.Path(), hookCfg.Env); err != nil {
		exitCode := shell.GetExitCode(err)
		b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", exitCode))

		// Give a simpler error if it's just a shell exit error
		if shell.IsExitError(err) {
			return &shell.ExitError{
				Code:    exitCode,
				Message: fmt.Sprintf("The %s hook exited with status %d", hookName, exitCode),
			}
		}
		return err
	}

	// Store the last hook exit code for subsequent steps
	b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", "0")

	// Get changed environment
	changes, err := script.Changes()
	if err != nil {
		// Could not compute the changes in environment or working directory
		// for some reason...

		switch err.(type) {
		case *hook.HookExitError:
			// ...because the hook called exit(), tsk we ignore any changes
			// since we can't discern them but continue on with the job
			break
		default:
			// ...because something else happened, report it and stop the job
			return fmt.Errorf("Failed to get environment: %w", err)
		}
	} else {
		// Hook exited successfully (and not early!) We have an environment and
		// wd change we can apply to our subsequent phases
		b.applyEnvironmentChanges(changes, redactors)
	}

	return nil
}

func (b *Executor) applyEnvironmentChanges(changes hook.HookScriptChanges, redactors redaction.RedactorMux) {
	if afterWd, err := changes.GetAfterWd(); err == nil {
		if afterWd != b.shell.Getwd() {
			_ = b.shell.Chdir(afterWd)
		}
	}

	// Do we even have any environment variables to change?
	if changes.Diff.Empty() {
		return
	}

	b.shell.Env.Apply(changes.Diff)

	// reset output redactors based on new environment variable values
	redactors.Flush()
	redactors.Reset(redaction.GetValuesToRedact(b.shell, b.Config.RedactedVars, b.shell.Env.Dump()))

	// First, let see any of the environment variables are supposed
	// to change the executor configuration at run time.
	executorConfigEnvChanges := b.Config.ReadFromEnvironment(b.shell.Env)

	// Print out the env vars that changed. As we go through each
	// one, we'll determine if it was a special "executor"
	// environment variable that has changed the executor
	// configuration at runtime.
	//
	// If it's "special", we'll show the value it was changed to -
	// otherwise we'll hide it. Since we don't know if an
	// environment variable contains sensitive information (such as
	// THIRD_PARTY_API_KEY) we'll just not show any values for
	// anything not controlled by us.
	for k, v := range changes.Diff.Added {
		if _, ok := executorConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s added", k)
		}
	}
	for k, v := range changes.Diff.Changed {
		if _, ok := executorConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s changed", k)
		}
	}
	for k, v := range changes.Diff.Removed {
		if _, ok := executorConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s removed", k)
		}
	}
}

func (b *Executor) hasGlobalHook(name string) bool {
	_, err := b.globalHookPath(name)
	return err == nil
}

// Returns the absolute path to a global hook, or os.ErrNotExist if none is found
func (b *Executor) globalHookPath(name string) (string, error) {
	return hook.Find(b.HooksPath, name)
}

// Executes a global hook if one exists
func (b *Executor) executeGlobalHook(ctx context.Context, name string) error {
	if !b.hasGlobalHook(name) {
		return nil
	}
	p, err := b.globalHookPath(name)
	if err != nil {
		return err
	}
	return b.executeHook(ctx, HookConfig{
		Scope: "global",
		Name:  name,
		Path:  p,
	})
}

// Returns the absolute path to a local hook, or os.ErrNotExist if none is found
func (b *Executor) localHookPath(name string) (string, error) {
	dir := filepath.Join(b.shell.Getwd(), ".buildkite", "hooks")
	return hook.Find(dir, name)
}

func (b *Executor) hasLocalHook(name string) bool {
	_, err := b.localHookPath(name)
	return err == nil
}

// Executes a local hook
func (b *Executor) executeLocalHook(ctx context.Context, name string) error {
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
	noLocalHooks, _ := b.shell.Env.Get("BUILDKITE_NO_LOCAL_HOOKS")
	if noLocalHooks == "true" || noLocalHooks == "1" {
		localHooksEnabled = false
	}

	if !localHooksEnabled {
		return fmt.Errorf("Refusing to run %s, local hooks are disabled", localHookPath)
	}

	return b.executeHook(ctx, HookConfig{
		Scope: "local",
		Name:  name,
		Path:  localHookPath,
	})
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
func addRepositoryHostToSSHKnownHosts(ctx context.Context, sh *shell.Shell, repository string) {
	if utils.FileExists(repository) {
		return
	}

	knownHosts, err := findKnownHosts(sh)
	if err != nil {
		sh.Warningf("Failed to find SSH known_hosts file: %v", err)
		return
	}

	if err = knownHosts.AddFromRepository(ctx, repository); err != nil {
		sh.Warningf("Error adding to known_hosts: %v", err)
		return
	}
}

// setUp is run before all the phases run. It's responsible for initializing the
// executor environment
func (b *Executor) setUp(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "environment", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if b.BinPath != "" {
		path, _ := b.shell.Env.Get("PATH")
		// BinPath goes last so we don't disturb other tools
		b.shell.Env.Set("PATH", fmt.Sprintf("%s%s%s", path, string(os.PathListSeparator), b.BinPath))
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

// tearDown is called before the executor exits, even on error
func (b *Executor) tearDown(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-exit", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

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
		return tearDownDeprecatedDockerIntegration(ctx, b.shell)
	}

	for _, dir := range b.cleanupDirs {
		if err = os.RemoveAll(dir); err != nil {
			b.shell.Warningf("Failed to remove dir %s: %v", dir, err)
		}
	}

	return nil
}

func (b *Executor) hasPlugins() bool {
	return b.Config.Plugins != ""
}

func (b *Executor) preparePlugins() error {
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
		return fmt.Errorf("Failed to parse a plugin definition: %w", err)
	}

	if b.Debug {
		b.shell.Commentf("Parsed %d plugins", len(b.plugins))
	}

	return nil
}

func (b *Executor) validatePluginCheckout(checkout *pluginCheckout) error {
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

		if errors.Is(err, plugin.ErrDefinitionNotFound) {
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
func (b *Executor) PluginPhase(ctx context.Context) error {
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

		checkout, err := b.checkoutPlugin(ctx, p)
		if err != nil {
			return fmt.Errorf("Failed to checkout plugin %s: %w", p.Name(), err)
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
func (b *Executor) VendoredPluginPhase(ctx context.Context) error {
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
			return fmt.Errorf("Failed to resolve vendored plugin path for plugin %s: %w", p.Name(), err)
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
func (b *Executor) executePluginHook(ctx context.Context, name string, checkouts []*pluginCheckout) error {
	for _, p := range checkouts {
		hookPath, err := hook.Find(p.HooksDir, name)
		if errors.Is(err, os.ErrNotExist) {
			continue // this plugin does not implement this hook
		} else if err != nil {
			return err
		}

		env, _ := p.ConfigurationToEnvironment()
		err = b.executeHook(ctx, HookConfig{
			Scope:      "plugin",
			Name:       name,
			Path:       hookPath,
			Env:        env,
			PluginName: p.Plugin.Name(),
			SpanAttributes: map[string]string{
				"plugin.name":        p.Plugin.Name(),
				"plugin.version":     p.Plugin.Version,
				"plugin.location":    p.Plugin.Location,
				"plugin.is_vendored": strconv.FormatBool(p.Vendored),
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// If any plugin has a hook by this name
func (b *Executor) hasPluginHook(name string) bool {
	for _, p := range b.pluginCheckouts {
		if _, err := hook.Find(p.HooksDir, name); err == nil {
			return true
		}
	}
	return false
}

// Checkout a given plugin to the plugins directory and return that directory
func (b *Executor) checkoutPlugin(ctx context.Context, p *plugin.Plugin) (*pluginCheckout, error) {
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
	// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
	if err := os.MkdirAll(b.PluginsPath, 0777); err != nil {
		return nil, err
	}

	// Create a path to the plugin
	pluginDirectory := filepath.Join(b.PluginsPath, id)
	pluginGitDirectory := filepath.Join(pluginDirectory, ".git")
	checkout := &pluginCheckout{
		Plugin:      p,
		CheckoutDir: pluginDirectory,
		HooksDir:    filepath.Join(pluginDirectory, "hooks"),
	}

	// Try and lock this particular plugin while we check it out (we create
	// the file outside of the plugin directory so git clone doesn't have
	// a cry about the directory not being empty)
	pluginCheckoutHook, err := b.shell.LockFile(ctx, filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
	if err != nil {
		return nil, err
	}
	defer pluginCheckoutHook.Unlock()

	// If there is already a clone, the user may want to ensure it's fresh (e.g., by setting
	// BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=true).
	//
	// Neither of the obvious options here is very nice.  Either we git-fetch and git-checkout on
	// existing repos, which is probably fast, but it's _surprisingly hard_ to write a really robust
	// chain of Git commands that'll definitely get you a clean version of a given upstream branch
	// ref (the branch might have been force-pushed, the checkout might have become dirty and
	// unmergeable, etc.).  Plus, then we're duplicating a bunch of fetch/checkout machinery and
	// perhaps missing things (like `addRepositoryHostToSSHKnownHosts` which is called down below).
	// Alternatively, we can DRY it up and simply `rm -rf` the plugin directory if it exists, but
	// that means a potentially slow and unnecessary clone on every build step.  Sigh.  I think the
	// tradeoff is favourable for just blowing away an existing clone if we want least-hassle
	// guarantee that the user will get the latest version of their plugin branch/tag/whatever.
	if b.Config.PluginsAlwaysCloneFresh && utils.FileExists(pluginDirectory) {
		b.shell.Commentf("BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH is true; removing previous checkout of plugin %s", p.Label())
		err = os.RemoveAll(pluginDirectory)
		if err != nil {
			b.shell.Errorf("Oh no, something went wrong removing %s", pluginDirectory)
			return nil, err
		}
	}

	if utils.FileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(ctx, b.shell, pluginDirectory, "--short=7", "HEAD")
		if err != nil {
			b.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			b.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		return checkout, nil
	}

	b.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, pluginDirectory)

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, b.shell, repo)
	}

	// Make the directory
	tempDir, err := os.MkdirTemp(b.PluginsPath, id)
	if err != nil {
		return nil, err
	}

	// Switch to the plugin directory
	b.shell.Commentf("Switching to the temporary plugin directory")
	previousWd := b.shell.Getwd()
	if err := b.shell.Chdir(tempDir); err != nil {
		return nil, err
	}
	// Switch back to the previous working directory
	defer b.shell.Chdir(previousWd)

	args := []string{"clone", "-v"}
	if b.GitSubmodules {
		// "--recursive" was added in Git 1.6.5, and is an alias to
		// "--recurse-submodules" from Git 2.13.
		args = append(args, "--recursive")
	}
	args = append(args, "--", repo, ".")

	// Plugin clones shouldn't use custom GitCloneFlags
	err = roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(2*time.Second)),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		return b.shell.Run(ctx, "git", args...)
	})
	if err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		b.shell.Commentf("Checking out `%s`", p.Version)
		if err = b.shell.Run(ctx, "git", "checkout", "-f", p.Version); err != nil {
			return nil, err
		}
	}

	b.shell.Commentf("Moving temporary plugin directory to final location")
	err = os.Rename(tempDir, pluginDirectory)
	if err != nil {
		return nil, err
	}

	return checkout, nil
}

func (b *Executor) removeCheckoutDir() error {
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

func (b *Executor) createCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !utils.FileExists(checkoutPath) {
		b.shell.Commentf("Creating \"%s\"", checkoutPath)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
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
func (b *Executor) CheckoutPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

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
		buildDir, err = os.MkdirTemp("", "buildkite-job-"+b.Config.JobID)
		if err != nil {
			return err
		}
		b.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", buildDir)

		// Track the directory so we can remove it at the end of the executor
		b.cleanupDirs = append(b.cleanupDirs, buildDir)
	}

	// Make sure the build directory exists
	if err := b.createCheckoutDir(); err != nil {
		return err
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case b.hasPluginHook("checkout"):
		if err := b.executePluginHook(ctx, "checkout", b.pluginCheckouts); err != nil {
			return err
		}
	case b.hasGlobalHook("checkout"):
		if err := b.executeGlobalHook(ctx, "checkout"); err != nil {
			return err
		}
	default:
		if b.Config.Repository != "" {
			err := roko.NewRetrier(
				roko.WithMaxAttempts(3),
				roko.WithStrategy(roko.Constant(2*time.Second)),
			).DoWithContext(ctx, func(r *roko.Retrier) error {
				err := b.defaultCheckoutPhase(ctx)
				if err == nil {
					return nil
				}

				switch {
				case shell.IsExitError(err) && shell.GetExitCode(err) == -1:
					b.shell.Warningf("Checkout was interrupted by a signal")
					r.Break()

				case errors.Is(err, context.Canceled):
					b.shell.Warningf("Checkout was cancelled")
					r.Break()

				case errors.Is(ctx.Err(), context.Canceled):
					b.shell.Warningf("Checkout was cancelled due to context cancellation")
					r.Break()

				default:
					b.shell.Warningf("Checkout failed! %s (%s)", err, r)

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
			})
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
	if err := b.executeGlobalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err := b.executeLocalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook(ctx, "post-checkout", b.pluginCheckouts); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		b.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err := b.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

func hasGitSubmodules(sh *shell.Shell) bool {
	return utils.FileExists(filepath.Join(sh.Getwd(), ".gitmodules"))
}

func hasGitCommit(ctx context.Context, sh *shell.Shell, gitDir string, commit string) bool {
	// Resolve commit to an actual commit object
	output, err := sh.RunAndCapture(ctx, "git", "--git-dir", gitDir, "rev-parse", commit+"^{commit}")
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

func (b *Executor) updateGitMirror(ctx context.Context, repository string) (string, error) {
	// Create a unique directory for the repository mirror
	mirrorDir := filepath.Join(b.Config.GitMirrorsPath, dirForRepository(repository))

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !utils.FileExists(baseDir) {
		b.shell.Commentf("Creating \"%s\"", baseDir)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
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
	mirrorCloneLock, err := b.shell.LockFile(ctx, mirrorDir+".clonelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorCloneLock.Unlock()

	// If we don't have a mirror, we need to clone it
	if !utils.FileExists(mirrorDir) {
		b.shell.Commentf("Cloning a mirror of the repository to %q", mirrorDir)
		flags := "--mirror " + b.GitCloneMirrorFlags
		if err := gitClone(ctx, b.shell, flags, repository, mirrorDir); err != nil {
			b.shell.Commentf("Removing mirror dir %q due to failed clone", mirrorDir)
			if err := os.RemoveAll(mirrorDir); err != nil {
				b.shell.Errorf("Failed to remove \"%s\" (%s)", mirrorDir, err)
			}
			return "", err
		}

		return mirrorDir, nil
	}

	// If it exists, immediately release the clone lock
	mirrorCloneLock.Unlock()

	// Check if the mirror has a commit, this is atomic so should be safe to do
	if hasGitCommit(ctx, b.shell, mirrorDir, b.Commit) {
		b.shell.Commentf("Commit %q exists in mirror", b.Commit)
		return mirrorDir, nil
	}

	if b.Debug {
		b.shell.Commentf("Acquiring mirror repository update lock")
	}

	// Lock the mirror dir to prevent concurrent updates
	mirrorUpdateLock, err := b.shell.LockFile(ctx, mirrorDir+".updatelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorUpdateLock.Unlock()

	// Check again after we get a lock, in case the other process has already updated
	if hasGitCommit(ctx, b.shell, mirrorDir, b.Commit) {
		b.shell.Commentf("Commit %q exists in mirror", b.Commit)
		return mirrorDir, nil
	}

	b.shell.Commentf("Updating existing repository mirror to find commit %s", b.Commit)

	// Update the origin of the repository so we can gracefully handle repository renames
	if err := b.shell.Run(ctx, "git", "--git-dir", mirrorDir, "remote", "set-url", "origin", b.Repository); err != nil {
		return "", err
	}

	if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and mirror pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)
		// Fetch the PR head from the upstream repository into the mirror.
		if err := b.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin", refspec); err != nil {
			return "", err
		}
	} else {
		// Fetch the build branch from the upstream repository into the mirror.
		if err := b.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin", b.Branch); err != nil {
			return "", err
		}
	}

	return mirrorDir, nil
}

func (b *Executor) getOrUpdateMirrorDir(ctx context.Context, repository string) (string, error) {
	var mirrorDir string
	var err error
	// Skip updating the Git mirror before using it?
	if b.Config.GitMirrorsSkipUpdate {
		mirrorDir = filepath.Join(b.Config.GitMirrorsPath, dirForRepository(repository))
		b.shell.Commentf("Skipping update and using existing mirror for repository %s at %s.", repository, mirrorDir)

		// Check if specified mirrorDir exists, otherwise the clone will fail.
		if !utils.FileExists(mirrorDir) {
			// Fall back to a clean clone, rather than failing the clone and therefore the build
			b.shell.Commentf("No existing mirror found for repository %s at %s.", repository, mirrorDir)
			mirrorDir = ""
		}
	} else {
		mirrorDir, err = b.updateGitMirror(ctx, repository)
		if err != nil {
			return "", err
		}
	}
	return mirrorDir, nil
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (b *Executor) defaultCheckoutPhase(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "repo-checkout", b.Config.TracingBackend)
	span.AddAttributes(map[string]string{
		"checkout.repo_name": b.Repository,
		"checkout.refspec":   b.RefSpec,
		"checkout.commit":    b.Commit,
	})
	var err error
	defer func() { span.FinishWithError(err) }()

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, b.shell, b.Repository)
	}

	var mirrorDir string

	// If we can, get a mirror of the git repository to use for reference later
	if experiments.IsEnabled(experiments.GitMirrors) && b.Config.GitMirrorsPath != "" && b.Config.Repository != "" {
		b.shell.Commentf("Using git-mirrors experiment 🧪")
		span.AddAttributes(map[string]string{"checkout.is_using_git_mirrors": "true"})
		mirrorDir, err = b.getOrUpdateMirrorDir(ctx, b.Repository)
		if err != nil {
			return fmt.Errorf("getting/updating git mirror: %w", err)
		}

		b.shell.Env.Set("BUILDKITE_REPO_MIRROR", mirrorDir)
	}

	// Make sure the build directory exists and that we change directory into it
	if err := b.createCheckoutDir(); err != nil {
		return fmt.Errorf("creating checkout dir: %w", err)
	}

	gitCloneFlags := b.GitCloneFlags
	if mirrorDir != "" {
		gitCloneFlags += fmt.Sprintf(" --reference %q", mirrorDir)
	}

	// Does the git directory exist?
	existingGitDir := filepath.Join(b.shell.Getwd(), ".git")
	if utils.FileExists(existingGitDir) {
		// Update the origin of the repository so we can gracefully handle repository renames
		if err := b.shell.Run(ctx, "git", "remote", "set-url", "origin", b.Repository); err != nil {
			return fmt.Errorf("setting origin: %w", err)
		}
	} else {
		if err := gitClone(ctx, b.shell, gitCloneFlags, b.Repository, "."); err != nil {
			return fmt.Errorf("cloning git repository: %w", err)
		}
	}

	// Git clean prior to checkout, we do this even if submodules have been
	// disabled to ensure previous submodules are cleaned up
	if hasGitSubmodules(b.shell) {
		if err := gitCleanSubmodules(ctx, b.shell, b.GitCleanFlags); err != nil {
			return fmt.Errorf("cleaning git submodules: %w", err)
		}
	}

	if err := gitClean(ctx, b.shell, b.GitCleanFlags); err != nil {
		return fmt.Errorf("cleaning git repository: %w", err)
	}

	gitFetchFlags := b.GitFetchFlags

	// If a refspec is provided then use it instead.
	// For example, `refs/not/a/head`
	if b.RefSpec != "" {
		b.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(ctx, b.shell, gitFetchFlags, "origin", b.RefSpec); err != nil {
			return fmt.Errorf("fetching refspec %q: %w", b.RefSpec, err)
		}

		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not there is a current head in this repository or another which
		// references the commit. We presume a commit sha is provided. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
	} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and checkout pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)

		if err := gitFetch(ctx, b.shell, gitFetchFlags, "origin", refspec); err != nil {
			return fmt.Errorf("fetching PR refspec %q: %w", refspec, err)
		}

		gitFetchHead, _ := b.shell.RunAndCapture(ctx, "git", "rev-parse", "FETCH_HEAD")
		b.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
	} else if b.Commit == "HEAD" {
		b.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(ctx, b.shell, gitFetchFlags, "origin", b.Branch); err != nil {
			return fmt.Errorf("fetching branch %q: %w", b.Branch, err)
		}

		// Otherwise fetch and checkout the commit directly. Some repositories don't
		// support fetching a specific commit so we fall back to fetching all heads
		// and tags, hoping that the commit is included.
	} else {
		if err := gitFetch(ctx, b.shell, gitFetchFlags, "origin", b.Commit); err != nil {
			// By default `git fetch origin` will only fetch tags which are
			// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
			// fetch all tags in addition to the default refspec, but pre 1.9.0 it
			// excludes the default refspec.
			gitFetchRefspec, _ := b.shell.RunAndCapture(ctx, "git", "config", "remote.origin.fetch")
			if err := gitFetch(ctx, b.shell, gitFetchFlags, "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*"); err != nil {
				return fmt.Errorf("fetching commit %q: %w", b.Commit, err)
			}
		}
	}

	gitCheckoutFlags := b.GitCheckoutFlags

	if b.Commit == "HEAD" {
		if err := gitCheckout(ctx, b.shell, gitCheckoutFlags, "FETCH_HEAD"); err != nil {
			return fmt.Errorf("checking out FETCH_HEAD: %w", err)
		}
	} else {
		if err := gitCheckout(ctx, b.shell, gitCheckoutFlags, b.Commit); err != nil {
			return fmt.Errorf("checking out commit %q: %w", b.Commit, err)
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
		// if the call fails, continue the executor
		// script, and show an informative error.
		if err := b.shell.Run(ctx, "git", "submodule", "sync", "--recursive"); err != nil {
			gitVersionOutput, _ := b.shell.RunAndCapture(ctx, "git", "--version")
			b.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		args := []string{}
		for _, config := range b.GitSubmoduleCloneConfig {
			args = append(args, "-c", config)
		}

		// Checking for submodule repositories
		submoduleRepos, err := gitEnumerateSubmoduleURLs(ctx, b.shell)
		if err != nil {
			b.shell.Warningf("Failed to enumerate git submodules: %v", err)
		} else {
			mirrorSubmodules := experiments.IsEnabled(experiments.GitMirrors) && b.Config.GitMirrorsPath != ""
			for _, repository := range submoduleRepos {
				submoduleArgs := append([]string(nil), args...)
				// submodules might need their fingerprints verified too
				if b.SSHKeyscan {
					addRepositoryHostToSSHKnownHosts(ctx, b.shell, repository)
				}

				if mirrorSubmodules {
					mirrorDir, err := b.getOrUpdateMirrorDir(ctx, repository)
					if err != nil {
						return fmt.Errorf("getting/updating mirror dir for submodules: %w", err)
					}

					// Switch back to the checkout dir, doing other operations from GitMirrorsPath will fail.
					if err := b.createCheckoutDir(); err != nil {
						return fmt.Errorf("creating checkout dir: %w", err)
					}

					// Tests use a local temp path for the repository, real repositories don't. Handle both.
					var repositoryPath string
					if !utils.FileExists(repository) {
						repositoryPath = filepath.Join(b.Config.GitMirrorsPath, dirForRepository(repository))
					} else {
						repositoryPath = repository
					}

					if mirrorDir != "" {
						submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force", "--reference", repositoryPath)
					} else {
						// Fall back to a clean update, rather than failing the checkout and therefore the build
						submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force")
					}

					if err := b.shell.Run(ctx, "git", submoduleArgs...); err != nil {
						return fmt.Errorf("updating submodules: %w", err)
					}
				}
			}

			if !mirrorSubmodules {
				args = append(args, "submodule", "update", "--init", "--recursive", "--force")
				if err := b.shell.Run(ctx, "git", args...); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			if err := b.shell.Run(ctx, "git", "submodule", "foreach", "--recursive", "git reset --hard"); err != nil {
				return fmt.Errorf("resetting submodules: %w", err)
			}
		}
	}

	// Git clean after checkout. We need to do this because submodules could have
	// changed in between the last checkout and this one. A double clean is the only
	// good solution to this problem that we've found
	b.shell.Commentf("Cleaning again to catch any post-checkout changes")

	if err := gitClean(ctx, b.shell, b.GitCleanFlags); err != nil {
		return fmt.Errorf("cleaning repository post-checkout: %w", err)
	}

	if gitSubmodules {
		if err := gitCleanSubmodules(ctx, b.shell, b.GitCleanFlags); err != nil {
			return fmt.Errorf("cleaning submodules post-checkout: %w", err)
		}
	}

	if _, hasToken := b.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN"); !hasToken {
		b.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// resolve BUILDKITE_COMMIT based on the local git repo
	if experiments.IsEnabled(experiments.ResolveCommitAfterCheckout) {
		b.shell.Commentf("Using resolve-commit-after-checkout experiment 🧪")
		b.resolveCommit(ctx)
	}

	// Grab author and commit information and send it back to Buildkite. But before we do, we'll check
	// to see if someone else has done it first.
	b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if err := b.shell.Run(ctx, "buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
		b.shell.Commentf("Sending Git commit information back to Buildkite")
		// Format:
		//
		// commit 0123456789abcdef0123456789abcdef01234567
		// abbrev-commit 0123456789
		// Author: John Citizen <john@example.com>
		//
		//    Subject of the commit message
		//
		//    Body of the commit message, which
		//    may span multiple lines.
		gitArgs := []string{
			"--no-pager",
			"show",
			"HEAD",
			"-s", // --no-patch was introduced in v1.8.4 in 2013, but e.g. CentOS 7 isn't there yet
			"--no-color",
			"--format=commit %H%nabbrev-commit %h%nAuthor: %an <%ae>%n%n%w(0,4,4)%B",
		}
		out, err := b.shell.RunAndCapture(ctx, "git", gitArgs...)
		if err != nil {
			return fmt.Errorf("getting git commit information: %w", err)
		}
		stdin := strings.NewReader(out)
		if err := b.shell.WithStdin(stdin).Run(ctx, "buildkite-agent", "meta-data", "set", "buildkite:git:commit"); err != nil {
			return fmt.Errorf("sending git commit information to Buildkite: %w", err)
		}
	}

	return nil
}

func (b *Executor) resolveCommit(ctx context.Context) {
	commitRef, _ := b.shell.Env.Get("BUILDKITE_COMMIT")
	if commitRef == "" {
		b.shell.Warningf("BUILDKITE_COMMIT was empty")
		return
	}
	cmdOut, err := b.shell.RunAndCapture(ctx, "git", "rev-parse", commitRef)
	if err != nil {
		b.shell.Warningf("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	if trimmedCmdOut != commitRef {
		b.shell.Commentf("Updating BUILDKITE_COMMIT from %q to %q", commitRef, trimmedCmdOut)
		b.shell.Env.Set("BUILDKITE_COMMIT", trimmedCmdOut)
	}
}

// runPreCommandHooks runs the pre-command hooks and adds tracing spans.
func (b *Executor) runPreCommandHooks(ctx context.Context) error {
	spanName := b.implementationSpecificSpanName("pre-command", "pre-command hooks")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

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
func (b *Executor) runCommand(ctx context.Context) error {
	var err error
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
func (b *Executor) runPostCommandHooks(ctx context.Context) error {
	spanName := b.implementationSpecificSpanName("post-command", "post-command hooks")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

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
func (b *Executor) CommandPhase(ctx context.Context) (error, error) {
	span, ctx := tracetools.StartSpanFromContext(ctx, "command", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()
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
func (b *Executor) defaultCommandPhase(ctx context.Context) error {
	spanName := b.implementationSpecificSpanName("default command hook", "hook.execute")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()
	span.AddAttributes(map[string]string{
		"hook.name": "command",
		"hook.type": "default",
	})

	// Make sure we actually have a command to run
	if strings.TrimSpace(b.Command) == "" {
		return fmt.Errorf("The command phase has no `command` to execute. Provide a `command` field in your step configuration, or define a `command` hook in a step plug-in, your repository `.buildkite/hooks`, or agent `hooks-path`.")
	}

	scriptFileName := strings.Replace(b.Command, "\n", "", -1)
	pathToCommand, err := filepath.Abs(filepath.Join(b.shell.Getwd(), scriptFileName))
	commandIsScript := err == nil && utils.FileExists(pathToCommand)
	span.AddAttributes(map[string]string{"hook.command": pathToCommand})

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
	shell, err := shellwords.Split(b.Shell)
	if err != nil {
		return fmt.Errorf("Failed to split shell (%q) into tokens: %v", b.Shell, err)
	}

	if len(shell) == 0 {
		return fmt.Errorf("No shell set for executor")
	}

	// Windows CMD.EXE is horrible and can't handle newline delimited commands. We write
	// a batch script so that it works, but we don't like it
	if strings.ToUpper(filepath.Base(shell[0])) == "CMD.EXE" {
		batchScript, err := b.writeBatchScript(b.Command)
		if err != nil {
			return err
		}
		defer os.Remove(batchScript)

		b.shell.Headerf("Running batch script")
		if b.Debug {
			contents, err := os.ReadFile(batchScript)
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
		err = runDeprecatedDockerIntegration(ctx, b.shell, []string{cmdToExec})
		return err
	}

	// If we aren't running a script, try and detect if we are using a posix shell
	// and if so add a trap so that the intermediate shell doesn't swallow signals
	// from cancellation
	if !commandIsScript && shellscript.IsPOSIXShell(b.Shell) {
		cmdToExec = fmt.Sprintf("trap 'kill -- $$' INT TERM QUIT; %s", cmdToExec)
	}

	redactors := b.setupRedactors()
	defer redactors.Flush()

	var cmd []string
	cmd = append(cmd, shell...)
	cmd = append(cmd, cmdToExec)

	if b.Debug {
		b.shell.Promptf("%s", process.FormatCommand(cmd[0], cmd[1:]))
	} else {
		b.shell.Promptf("%s", cmdToExec)
	}

	err = b.shell.RunWithoutPrompt(ctx, cmd[0], cmd[1:]...)
	return err
}

/*
If line is another batch script, it should be prefixed with `call ` so that
the second batch script doesn’t early exit our calling script.

See https://www.robvanderwoude.com/call.php
*/
func shouldCallBatchLine(line string) bool {
	// "  	gubiwargiub.bat /S  /e -e foo"
	// "    "

	/*
		1. Trim leading whitespace characters
		2. Split on whitespace into an array
		3. Take the first element
		4. If element ends in .bat or .cmd (case insensitive), the line should be prefixed, else not.
	*/

	trim := strings.TrimSpace(line) // string

	elements := strings.Fields(trim) // []string

	if len(elements) < 1 {
		return false
	}

	first := strings.ToLower(elements[0]) // string

	return (strings.HasSuffix(first, ".bat") || strings.HasSuffix(first, ".cmd"))
}

func (b *Executor) writeBatchScript(cmd string) (string, error) {
	scriptFile, err := shell.TempFileWithExtension(
		"buildkite-script.bat",
	)
	if err != nil {
		return "", err
	}
	defer scriptFile.Close()

	var scriptContents = []string{"@echo off"}

	for _, line := range strings.Split(cmd, "\n") {
		if line != "" {
			if shouldCallBatchLine(line) {
				scriptContents = append(scriptContents, "call "+line)
			} else {
				scriptContents = append(scriptContents, line)
			}
			scriptContents = append(scriptContents, "if %errorlevel% neq 0 exit /b %errorlevel%")
		}
	}

	_, err = io.WriteString(scriptFile, strings.Join(scriptContents, "\n"))
	if err != nil {
		return "", err
	}

	return scriptFile.Name(), nil

}

func (b *Executor) artifactPhase(ctx context.Context) error {
	if b.AutomaticArtifactUploadPaths == "" {
		return nil
	}

	spanName := b.implementationSpecificSpanName("artifacts", "artifact upload")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	err = b.preArtifactHooks(ctx)
	if err != nil {
		return err
	}

	err = b.uploadArtifacts(ctx)
	if err != nil {
		return err
	}

	err = b.postArtifactHooks(ctx)
	if err != nil {
		return err
	}

	return nil
}

// Run the pre-artifact hooks
func (b *Executor) preArtifactHooks(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-artifact", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = b.executeGlobalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "pre-artifact", b.pluginCheckouts); err != nil {
		return err
	}

	return nil
}

// Run the artifact upload command
func (b *Executor) uploadArtifacts(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "artifact-upload", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	b.shell.Headerf("Uploading artifacts")
	args := []string{"artifact", "upload", b.AutomaticArtifactUploadPaths}

	// If blank, the upload destination is buildkite
	if b.ArtifactUploadDestination != "" {
		args = append(args, b.ArtifactUploadDestination)
	}

	if err = b.shell.Run(ctx, "buildkite-agent", args...); err != nil {
		return err
	}

	return nil
}

// Run the post-artifact hooks
func (b *Executor) postArtifactHooks(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "post-artifact", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

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
// env (for example, BUILDKITE_BUILD_PATH) can only be set from config or by hooks.
// If these env are set at a pipeline level, we rewrite them to BUILDKITE_X_BUILD_PATH
// and warn on them here so that users know what is going on
func (b *Executor) ignoredEnv() []string {
	var ignored []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "BUILDKITE_X_") {
			ignored = append(ignored, fmt.Sprintf("BUILDKITE_%s",
				strings.TrimPrefix(env, "BUILDKITE_X_")))
		}
	}
	return ignored
}

// setupRedactors wraps shell output and logging in Redactor if any redaction
// is necessary based on RedactedVars configuration and the existence of
// matching environment vars.
// redaction.RedactorMux (possibly empty) is returned so the caller can `defer redactor.Flush()`
func (b *Executor) setupRedactors() redaction.RedactorMux {
	valuesToRedact := redaction.GetValuesToRedact(b.shell, b.Config.RedactedVars, b.shell.Env.Dump())
	if len(valuesToRedact) == 0 {
		return nil
	}

	if b.Debug {
		b.shell.Commentf("Enabling output redaction for values from environment variables matching: %v", b.Config.RedactedVars)
	}

	var mux redaction.RedactorMux

	// If the shell Writer is already a Redactor, reset the values to redact.
	if redactor, ok := b.shell.Writer.(*redaction.Redactor); ok {
		redactor.Reset(valuesToRedact)
		mux = append(mux, redactor)
	} else if len(valuesToRedact) == 0 {
		// skip
	} else {
		redactor := redaction.NewRedactor(b.shell.Writer, "[REDACTED]", valuesToRedact)
		b.shell.Writer = redactor
		mux = append(mux, redactor)
	}

	// If the shell.Logger is already a redacted WriterLogger, reset the values to redact.
	// (maybe there's a better way to do two levels of type assertion? ...
	// shell.Logger may be a WriterLogger, and its Writer may be a Redactor)
	var shellWriterLogger *shell.WriterLogger
	var shellLoggerRedactor *redaction.Redactor
	if logger, ok := b.shell.Logger.(*shell.WriterLogger); ok {
		shellWriterLogger = logger
		if redactor, ok := logger.Writer.(*redaction.Redactor); ok {
			shellLoggerRedactor = redactor
		}
	}
	if redactor := shellLoggerRedactor; redactor != nil {
		redactor.Reset(valuesToRedact)
		mux = append(mux, redactor)
	} else if len(valuesToRedact) == 0 {
		// skip
	} else if shellWriterLogger != nil {
		redactor := redaction.NewRedactor(b.shell.Writer, "[REDACTED]", valuesToRedact)
		shellWriterLogger.Writer = redactor
		mux = append(mux, redactor)
	}

	return mux
}

type pluginCheckout struct {
	*plugin.Plugin
	*plugin.Definition
	CheckoutDir string
	HooksDir    string
}

func (b *Executor) startKubernetesClient(ctx context.Context, kubernetesClient *kubernetes.Client) error {
	b.shell.Commentf("Using experimental Kubernetes support")
	err := roko.NewRetrier(
		roko.WithMaxAttempts(7),
		roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
	).Do(func(rtr *roko.Retrier) error {
		id, err := strconv.Atoi(os.Getenv("BUILDKITE_CONTAINER_ID"))
		if err != nil {
			return fmt.Errorf("failed to parse container id, %s", os.Getenv("BUILDKITE_CONTAINER_ID"))
		}
		kubernetesClient.ID = id
		connect, err := kubernetesClient.Connect()
		if err != nil {
			return err
		}
		os.Setenv("BUILDKITE_AGENT_ACCESS_TOKEN", connect.AccessToken)
		b.shell.Env.Set("BUILDKITE_AGENT_ACCESS_TOKEN", connect.AccessToken)
		writer := io.MultiWriter(os.Stdout, kubernetesClient)
		b.shell.Writer = writer
		b.shell.Logger = &shell.WriterLogger{
			Writer: writer,
			Ansi:   true,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error connecting to kubernetes runner: %w", err)
	}
	if err := kubernetesClient.Await(ctx, kubernetes.RunStateStart); err != nil {
		return fmt.Errorf("error waiting for client to become ready: %w", err)
	}
	go func() {
		if err := kubernetesClient.Await(ctx, kubernetes.RunStateInterrupt); err != nil {
			b.shell.Errorf("Error waiting for client interrupt: %v", err)
		}
		b.cancelCh <- struct{}{}
	}()
	return nil
}
