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
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/shellscript"
	"github.com/buildkite/agent/v3/internal/utils"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

// Executor represents the phases of execution in a Buildkite Job. It's run as
// a sub-process of the buildkite-agent and finishes at the conclusion of a job.
//
// Historically (prior to v3) the job executor was a shell script, but was ported
// to Go for portability and testability.
type Executor struct {
	// ExecutorConfig provides the executor configuration
	ExecutorConfig

	// Shell is the shell environment for the executor
	shell *shell.Shell

	// Plugins to use
	plugins []*plugin.Plugin

	// Plugin checkouts from the plugin phases
	pluginCheckouts []*pluginCheckout

	// Directories to clean up at end of job execution
	cleanupDirs []string

	// A channel to track cancellation
	cancelCh chan struct{}
}

// New returns a new executor instance
func New(conf ExecutorConfig) *Executor {
	return &Executor{
		ExecutorConfig: conf,
		cancelCh:       make(chan struct{}),
	}
}

// Run the job and return the exit code
func (e *Executor) Run(ctx context.Context) (exitCode int) {
	// Check if not nil to allow for tests to overwrite shell
	if e.shell == nil {
		var err error
		e.shell, err = shell.New()
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			return 1
		}

		e.shell.PTY = e.ExecutorConfig.RunInPty
		e.shell.Debug = e.ExecutorConfig.Debug
		e.shell.InterruptSignal = e.ExecutorConfig.CancelSignal
		e.shell.SignalGracePeriod = e.ExecutorConfig.SignalGracePeriod
	}
	if experiments.IsEnabled(experiments.KubernetesExec) {
		kubernetesClient := &kubernetes.Client{}
		if err := e.startKubernetesClient(ctx, kubernetesClient); err != nil {
			e.shell.Errorf("Failed to start kubernetes client: %v", err)
			return 1
		}
		defer func() {
			kubernetesClient.Exit(exitCode)
		}()
	}

	var err error
	span, ctx, stopper := e.startTracing(ctx)
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

		case <-e.cancelCh:
			e.shell.Commentf("Received cancellation signal, interrupting")
			e.shell.Interrupt()
			cancel()
		}
	}()

	// Create an empty env for us to keep track of our env changes in
	e.shell.Env = env.FromSlice(os.Environ())

	// Initialize the job API, iff the experiment is enabled. Noop otherwise
	cleanup, err := e.startJobAPI()
	if err != nil {
		e.shell.Errorf("Error setting up job API: %v", err)
		return 1
	}

	defer cleanup()

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		if err = e.tearDown(ctx); err != nil {
			e.shell.Errorf("Error tearing down job executor: %v", err)

			// this gets passed back via the named return
			exitCode = shell.GetExitCode(err)
		}
	}()

	// Initialize the environment, a failure here will still call the tearDown
	if err = e.setUp(ctx); err != nil {
		e.shell.Errorf("Error setting up job executor: %v", err)
		return shell.GetExitCode(err)
	}

	var includePhase = func(phase string) bool {
		if len(e.Phases) == 0 {
			return true
		}
		for _, include := range e.Phases {
			if include == phase {
				return true
			}
		}
		return false
	}

	// Execute the job phases in order
	var phaseErr error

	if includePhase("plugin") {
		phaseErr = e.preparePlugins()

		if phaseErr == nil {
			phaseErr = e.PluginPhase(ctx)
		}
	}

	if phaseErr == nil && includePhase("checkout") {
		phaseErr = e.CheckoutPhase(cancelCtx)
	} else {
		checkoutDir, exists := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
		if exists {
			_ = e.shell.Chdir(checkoutDir)
		}
	}

	if phaseErr == nil && includePhase("plugin") {
		phaseErr = e.VendoredPluginPhase(ctx)
	}

	if phaseErr == nil && includePhase("command") {
		var commandErr error
		phaseErr, commandErr = e.CommandPhase(ctx)
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
			e.shell.Printf("user command error: %v", commandErr)
			span.RecordError(commandErr)
		}

		// Only upload artifacts as part of the command phase
		if err = e.artifactPhase(ctx); err != nil {
			e.shell.Errorf("%v", err)

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
		e.shell.Errorf("%v", phaseErr)
		return shell.GetExitCode(phaseErr)
	}

	// Use the exit code from the command phase
	exitStatus, _ := e.shell.Env.Get("BUILDKITE_COMMAND_EXIT_STATUS")
	exitStatusCode, _ := strconv.Atoi(exitStatus)

	return exitStatusCode
}

// Cancel interrupts any running shell processes and causes the job to stop
func (e *Executor) Cancel() error {
	e.cancelCh <- struct{}{}
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

func (e *Executor) tracingImplementationSpecificHookScope(scope string) string {
	if e.TracingBackend != tracetools.BackendOpenTelemetry {
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
func (e *Executor) executeHook(ctx context.Context, hookCfg HookConfig) error {
	scopeName := e.tracingImplementationSpecificHookScope(hookCfg.Scope)
	spanName := e.implementationSpecificSpanName(fmt.Sprintf("%s %s hook", scopeName, hookCfg.Name), "hook.execute")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
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
		if e.Debug {
			e.shell.Commentf("Skipping %s hook, no script at \"%s\"", hookName, hookCfg.Path)
		}
		return nil
	}

	e.shell.Headerf("Running %s hook", hookName)

	if !experiments.IsEnabled(experiments.PolyglotHooks) {
		return e.runWrappedShellScriptHook(ctx, hookName, hookCfg)
	}

	hookType, err := hook.Type(hookCfg.Path)
	if err != nil {
		return fmt.Errorf("determining hook type for %q hook: %w", hookName, err)
	}

	switch hookType {
	case hook.TypeScript:
		if runtime.GOOS == "windows" {
			// We use shebangs to figure out how to run scripts, and Windows has no way to interpret a shebang
			// ie, on linux, we can just point the OS to a file of some sort and say "run that", and as part of that it will try to
			// read a shebang, and run the script using the interpreter specified. Windows can't do this, so we can't run scripts
			// directly on Windows

			// Potentially there's something that we could do with file extensions, but that's a bit of a minefield, and would
			// probably mean that we have to have a list of blessed hook runtimes on windows only... or something.

			// Regardless: not supported right now, or potentially ever.
			sheb, _ := shellscript.ShebangLine(hookCfg.Path) // we know this won't error because it must have a shebang to be a script

			err := fmt.Errorf(`when trying to run the hook at %q, the agent found that it was a script with a shebang that isn't for a shellscripting language - in this case, %q.
Hooks of this kind are unfortunately not supported on Windows, as we have no way of interpreting a shebang on Windows`, hookCfg.Path, sheb)
			return err
		}

		// It's a script, and we can rely on the OS to figure out how to run it (because we're not on windows), so run it
		// directly without wrapping
		if err := e.runUnwrappedHook(ctx, hookName, hookCfg); err != nil {
			return fmt.Errorf("running %q script hook: %w", hookName, err)
		}

		return nil
	case hook.TypeBinary:
		// It's a binary, so we'll just run it directly, no wrapping needed or possible
		if err := e.runUnwrappedHook(ctx, hookName, hookCfg); err != nil {
			return fmt.Errorf("running %q binary hook: %w", hookName, err)
		}

		return nil
	case hook.TypeShell:
		// It's definitely a shell script, wrap it so that we can snaffle the changed environment variables
		if err := e.runWrappedShellScriptHook(ctx, hookName, hookCfg); err != nil {
			return fmt.Errorf("running %q shell hook: %w", hookName, err)
		}

		return nil
	default:
		return fmt.Errorf("unknown hook type %q for %q hook", hookType, hookName)
	}
}

func (e *Executor) runUnwrappedHook(ctx context.Context, hookName string, hookCfg HookConfig) error {
	environ := hookCfg.Env.Copy()

	environ.Set("BUILDKITE_HOOK_PHASE", hookCfg.Name)
	environ.Set("BUILDKITE_HOOK_PATH", hookCfg.Path)
	environ.Set("BUILDKITE_HOOK_SCOPE", hookCfg.Scope)

	return e.shell.RunWithEnv(ctx, environ, hookCfg.Path)
}

func (e *Executor) runWrappedShellScriptHook(ctx context.Context, hookName string, hookCfg HookConfig) error {
	redactors := e.setupRedactors()
	defer redactors.Flush()

	script, err := hook.NewScriptWrapper(hook.WithHookPath(hookCfg.Path))
	if err != nil {
		e.shell.Errorf("Error creating hook script: %v", err)
		return err
	}
	defer script.Close()

	cleanHookPath := hookCfg.Path

	// Show a relative path if we can
	if strings.HasPrefix(hookCfg.Path, e.shell.Getwd()) {
		var err error
		if cleanHookPath, err = filepath.Rel(e.shell.Getwd(), hookCfg.Path); err != nil {
			cleanHookPath = hookCfg.Path
		}
	}

	// Show the hook runner in debug, but the thing being run otherwise 💅🏻
	if e.Debug {
		e.shell.Commentf("A hook runner was written to \"%s\" with the following:", script.Path())
		e.shell.Promptf("%s", process.FormatCommand(script.Path(), nil))
	} else {
		e.shell.Promptf("%s", process.FormatCommand(cleanHookPath, []string{}))
	}

	// Run the wrapper script
	if err = e.shell.RunScript(ctx, script.Path(), hookCfg.Env); err != nil {
		exitCode := shell.GetExitCode(err)
		e.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", exitCode))

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
	e.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", "0")

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
		e.applyEnvironmentChanges(changes, redactors)
	}

	return nil
}

func (e *Executor) applyEnvironmentChanges(changes hook.HookScriptChanges, redactors replacer.Mux) {
	if afterWd, err := changes.GetAfterWd(); err == nil {
		if afterWd != e.shell.Getwd() {
			_ = e.shell.Chdir(afterWd)
		}
	}

	// Do we even have any environment variables to change?
	if changes.Diff.Empty() {
		return
	}

	e.shell.Env.Apply(changes.Diff)

	// reset output redactors based on new environment variable values
	redactors.Reset(redact.Values(e.shell, e.ExecutorConfig.RedactedVars, e.shell.Env.Dump()))

	// First, let see any of the environment variables are supposed
	// to change the job configuration at run time.
	executorConfigEnvChanges := e.ExecutorConfig.ReadFromEnvironment(e.shell.Env)

	// Print out the env vars that changed. As we go through each
	// one, we'll determine if it was a special environment variable
	// that has changed the executor configuration at runtime.
	//
	// If it's "special", we'll show the value it was changed to -
	// otherwise we'll hide it. Since we don't know if an
	// environment variable contains sensitive information (such as
	// THIRD_PARTY_API_KEY) we'll just not show any values for
	// anything not controlled by us.
	for k, v := range changes.Diff.Added {
		if _, ok := executorConfigEnvChanges[k]; ok {
			e.shell.Commentf("%s is now %q", k, v)
		} else {
			e.shell.Commentf("%s added", k)
		}
	}
	for k, v := range changes.Diff.Changed {
		if _, ok := executorConfigEnvChanges[k]; ok {
			e.shell.Commentf("%s is now %q", k, v)
		} else {
			e.shell.Commentf("%s changed", k)
		}
	}
	for k, v := range changes.Diff.Removed {
		if _, ok := executorConfigEnvChanges[k]; ok {
			e.shell.Commentf("%s is now %q", k, v)
		} else {
			e.shell.Commentf("%s removed", k)
		}
	}
}

func (e *Executor) hasGlobalHook(name string) bool {
	_, err := e.globalHookPath(name)
	return err == nil
}

// Returns the absolute path to a global hook, or os.ErrNotExist if none is found
func (e *Executor) globalHookPath(name string) (string, error) {
	return hook.Find(e.HooksPath, name)
}

// Executes a global hook if one exists
func (e *Executor) executeGlobalHook(ctx context.Context, name string) error {
	if !e.hasGlobalHook(name) {
		return nil
	}
	p, err := e.globalHookPath(name)
	if err != nil {
		return err
	}
	return e.executeHook(ctx, HookConfig{
		Scope: "global",
		Name:  name,
		Path:  p,
	})
}

// Returns the absolute path to a local hook, or os.ErrNotExist if none is found
func (e *Executor) localHookPath(name string) (string, error) {
	dir := filepath.Join(e.shell.Getwd(), ".buildkite", "hooks")
	return hook.Find(dir, name)
}

func (e *Executor) hasLocalHook(name string) bool {
	_, err := e.localHookPath(name)
	return err == nil
}

// Executes a local hook
func (e *Executor) executeLocalHook(ctx context.Context, name string) error {
	localHookPath, err := e.localHookPath(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// If the hook doesn't exist, that's fine, we'll just skip it
			if e.ExecutorConfig.Debug {
				e.shell.Logger.Commentf("Local hook %s doesn't exist: %s, skipping", name, err)
			}
			return nil
		}

		// This should not be possible under the current state of the code base
		// as hook.Find only returns os.ErrNotExist but that assumes implementation
		// details that could change in the future
		return err
	}

	// For high-security configs, we allow the disabling of local hooks.
	localHooksEnabled := e.ExecutorConfig.LocalHooksEnabled

	// Allow hooks to disable local hooks by setting BUILDKITE_NO_LOCAL_HOOKS=true
	noLocalHooks, _ := e.shell.Env.Get("BUILDKITE_NO_LOCAL_HOOKS")
	if noLocalHooks == "true" || noLocalHooks == "1" {
		localHooksEnabled = false
	}

	if !localHooksEnabled {
		return fmt.Errorf("Refusing to run %s, local hooks are disabled", localHookPath)
	}

	return e.executeHook(ctx, HookConfig{
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
// job environment
func (e *Executor) setUp(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "environment", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if e.BinPath != "" {
		path, _ := e.shell.Env.Get("PATH")
		// BinPath goes last so we don't disturb other tools
		e.shell.Env.Set("PATH", fmt.Sprintf("%s%s%s", path, string(os.PathListSeparator), e.BinPath))
	}

	// Set a BUILDKITE_BUILD_CHECKOUT_PATH unless one exists already. We do this here
	// so that the environment will have a checkout path to work with
	if _, exists := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"); !exists {
		if e.BuildPath == "" {
			return fmt.Errorf("Must set either a BUILDKITE_BUILD_PATH or a BUILDKITE_BUILD_CHECKOUT_PATH")
		}
		e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH",
			filepath.Join(e.BuildPath, dirForAgentName(e.AgentName), e.OrganizationSlug, e.PipelineSlug))
	}

	// The job runner sets BUILDKITE_IGNORED_ENV with any keys that were ignored
	// or overwritten. This shows a warning to the user so they don't get confused
	// when their environment changes don't seem to do anything
	if ignored, exists := e.shell.Env.Get("BUILDKITE_IGNORED_ENV"); exists {
		e.shell.Headerf("Detected protected environment variables")
		e.shell.Commentf("Your pipeline environment has protected environment variables set. " +
			"These can only be set via hooks, plugins or the agent configuration.")

		for _, env := range strings.Split(ignored, ",") {
			e.shell.Warningf("Ignored %s", env)
		}

		e.shell.Printf("^^^ +++")
	}

	if e.Debug {
		e.shell.Headerf("Buildkite environment variables")
		for _, envar := range e.shell.Env.ToSlice() {
			if strings.HasPrefix(envar, "BUILDKITE_AGENT_ACCESS_TOKEN=") {
				e.shell.Printf("BUILDKITE_AGENT_ACCESS_TOKEN=******************")
			} else if strings.HasPrefix(envar, "BUILDKITE") || strings.HasPrefix(envar, "CI") || strings.HasPrefix(envar, "PATH") {
				e.shell.Printf("%s", strings.Replace(envar, "\n", "\\n", -1))
			}
		}
	}

	// Disable any interactive Git/SSH prompting
	e.shell.Env.Set("GIT_TERMINAL_PROMPT", "0")

	// It's important to do this before checking out plugins, in case you want
	// to use the global environment hook to whitelist the plugins that are
	// allowed to be used.
	err = e.executeGlobalHook(ctx, "environment")
	return err
}

// tearDown is called before the executor exits, even on error
func (e *Executor) tearDown(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-exit", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "pre-exit"); err != nil {
		return err
	}

	if err = e.executeLocalHook(ctx, "pre-exit"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "pre-exit", e.pluginCheckouts); err != nil {
		return err
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(e.shell) {
		return tearDownDeprecatedDockerIntegration(ctx, e.shell)
	}

	for _, dir := range e.cleanupDirs {
		if err = os.RemoveAll(dir); err != nil {
			e.shell.Warningf("Failed to remove dir %s: %v", dir, err)
		}
	}

	return nil
}

func (e *Executor) hasPlugins() bool {
	return e.ExecutorConfig.Plugins != ""
}

func (e *Executor) preparePlugins() error {
	if !e.hasPlugins() {
		return nil
	}

	e.shell.Headerf("Preparing plugins")

	if e.Debug {
		e.shell.Commentf("Plugin JSON is %s", e.Plugins)
	}

	// Check if we can run plugins (disabled via --no-plugins)
	if !e.ExecutorConfig.PluginsEnabled {
		if !e.ExecutorConfig.LocalHooksEnabled {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-local-hooks`")
		} else if !e.ExecutorConfig.CommandEval {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-command-eval`")
		} else {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-plugins`")
		}
	}

	var err error
	e.plugins, err = plugin.CreateFromJSON(e.ExecutorConfig.Plugins)
	if err != nil {
		return fmt.Errorf("Failed to parse a plugin definition: %w", err)
	}

	if e.Debug {
		e.shell.Commentf("Parsed %d plugins", len(e.plugins))
	}

	return nil
}

func (e *Executor) validatePluginCheckout(ctx context.Context, checkout *pluginCheckout) error {
	if !e.ExecutorConfig.PluginValidation {
		return nil
	}

	if checkout.Definition == nil {
		if e.Debug {
			e.shell.Commentf("Parsing plugin definition for %s from %s", checkout.Plugin.Name(), checkout.CheckoutDir)
		}

		// parse the plugin definition from the plugin checkout dir
		var err error
		checkout.Definition, err = plugin.LoadDefinitionFromDir(checkout.CheckoutDir)

		if errors.Is(err, plugin.ErrDefinitionNotFound) {
			e.shell.Warningf("Failed to find plugin definition for plugin %s", checkout.Plugin.Name())
			return nil
		} else if err != nil {
			return err
		}
	}

	val := &plugin.Validator{}
	result := val.Validate(ctx, checkout.Definition, checkout.Plugin.Configuration)

	if !result.Valid() {
		e.shell.Headerf("Plugin validation failed for %q", checkout.Plugin.Name())
		json, _ := json.Marshal(checkout.Plugin.Configuration)
		e.shell.Commentf("Plugin configuration JSON is %s", json)
		return result
	}

	e.shell.Commentf("Valid plugin configuration for %q", checkout.Plugin.Name())
	return nil
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (e *Executor) PluginPhase(ctx context.Context) error {
	if len(e.plugins) == 0 {
		if e.Debug {
			e.shell.Commentf("Skipping plugin phase")
		}
		return nil
	}

	checkoutPluginMethod := e.checkoutPlugin
	if experiments.IsEnabled(experiments.IsolatedPluginCheckout) {
		if e.Debug {
			e.shell.Commentf("Using isolated plugin checkout")
		}
		checkoutPluginMethod = e.checkoutPluginIsolated
	}

	checkouts := []*pluginCheckout{}

	// Checkout and validate plugins that aren't vendored
	for _, p := range e.plugins {
		if p.Vendored {
			if e.Debug {
				e.shell.Commentf("Skipping vendored plugin %s", p.Name())
			}
			continue
		}

		checkout, err := checkoutPluginMethod(ctx, p)
		if err != nil {
			return fmt.Errorf("Failed to checkout plugin %s: %w", p.Name(), err)
		}

		err = e.validatePluginCheckout(ctx, checkout)
		if err != nil {
			return err
		}

		checkouts = append(checkouts, checkout)
	}

	// Store the checkouts for future use
	e.pluginCheckouts = checkouts

	// Now we can run plugin environment hooks too
	return e.executePluginHook(ctx, "environment", checkouts)
}

// VendoredPluginPhase is where plugins that are included in the
// checked out code are added
func (e *Executor) VendoredPluginPhase(ctx context.Context) error {
	if !e.hasPlugins() {
		return nil
	}

	vendoredCheckouts := []*pluginCheckout{}

	// Validate vendored plugins
	for _, p := range e.plugins {
		if !p.Vendored {
			continue
		}

		checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

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

		err = e.validatePluginCheckout(ctx, checkout)
		if err != nil {
			return err
		}

		vendoredCheckouts = append(vendoredCheckouts, checkout)
	}

	// Finally append our vendored checkouts to the rest for subsequent hooks
	e.pluginCheckouts = append(e.pluginCheckouts, vendoredCheckouts...)

	// Now we can run plugin environment hooks too
	return e.executePluginHook(ctx, "environment", vendoredCheckouts)
}

// Hook types that we should only run one of, but a long-standing bug means that
// we allowed more than one to run (for plugins).
var strictSingleHookTypes = map[string]bool{
	"command":  true,
	"checkout": true,
}

// Executes a named hook on plugins that have it
func (e *Executor) executePluginHook(ctx context.Context, name string, checkouts []*pluginCheckout) error {
	// Command and checkout hooks are a little different, in that we only execute
	// the first one we see. We run the first one, and output a warning for all
	// the subsequent ones.
	hookTypeSeen := make(map[string]bool)

	for i, p := range checkouts {
		hookPath, err := hook.Find(p.HooksDir, name)
		if errors.Is(err, os.ErrNotExist) {
			continue // this plugin does not implement this hook
		}
		if err != nil {
			return err
		}

		if strictSingleHookTypes[name] && hookTypeSeen[name] {
			if e.ExecutorConfig.StrictSingleHooks {
				e.shell.Logger.Warningf("Ignoring additional %s hook (%s plugin, position %d)",
					name, p.Plugin.Name(), i+1)
				continue
			} else {
				e.shell.Logger.Warningf("The additional %s hook (%s plugin, position %d) "+
					"will be ignored in a future version of the agent. To enforce "+
					"single %s hooks now, pass the --strict-single-hooks flag, set "+
					"the environment variable BUILDKITE_STRICT_SINGLE_HOOKS=true, "+
					"or set strict-single-hooks=true in your agent configuration",
					name, p.Plugin.Name(), i+1, name)
			}
		}
		hookTypeSeen[name] = true

		envMap, err := p.ConfigurationToEnvironment()
		if dnerr := (&plugin.DeprecatedNameErrors{}); errors.As(err, &dnerr) {
			e.shell.Logger.Headerf("Deprecated environment variables for plugin %s", p.Plugin.Name())
			e.shell.Logger.Printf("%s", strings.Join([]string{
				"The way that environment variables are derived from the plugin configuration is changing.",
				"We'll export both the deprecated and the replacement names for now,",
				"You may be able to avoid this by removing consecutive underscore, hyphen, or whitespace",
				"characters in your plugin configuration.",
			}, " "))
			for _, err := range dnerr.Unwrap() {
				e.shell.Logger.Printf("%s", err.Error())
			}
		} else if err != nil {
			e.shell.Logger.Warningf("Error configuring plugin environment: %s", err)
		}

		if err := e.executeHook(ctx, HookConfig{
			Scope:      "plugin",
			Name:       name,
			Path:       hookPath,
			Env:        envMap,
			PluginName: p.Plugin.Name(),
			SpanAttributes: map[string]string{
				"plugin.name":        p.Plugin.Name(),
				"plugin.version":     p.Plugin.Version,
				"plugin.location":    p.Plugin.Location,
				"plugin.is_vendored": strconv.FormatBool(p.Vendored),
			},
		}); err != nil {
			return err
		}
	}

	return nil
}

// If any plugin has a hook by this name
func (e *Executor) hasPluginHook(name string) bool {
	for _, p := range e.pluginCheckouts {
		if _, err := hook.Find(p.HooksDir, name); err == nil {
			return true
		}
	}
	return false
}

// Checkout a given plugin to the plugins directory and return that directory. Each agent worker
// will checkout the plugin to a different directory, so that they don't conflict with each other.
func (e *Executor) checkoutPluginIsolated(ctx context.Context, p *plugin.Plugin) (*pluginCheckout, error) {
	// Make sure we have a plugin path before trying to do anything
	if e.PluginsPath == "" {
		return nil, fmt.Errorf("Can't checkout plugin without a `plugins-path`")
	}

	id, err := p.Identifier()
	if err != nil {
		return nil, err
	}

	pluginParentDir := filepath.Join(e.PluginsPath, e.AgentName)

	// Ensure the parent of the plugin directory exists, otherwise we can't move the temp git repo dir
	// into it. The actual file permissions will be reduced by umask, and won't be 0o777 unless the
	// user has manually changed the umask to 0o000
	if err := os.MkdirAll(pluginParentDir, 0o777); err != nil {
		return nil, err
	}

	// Create a path to the plugin
	pluginDirectory := filepath.Join(pluginParentDir, id)
	pluginGitDirectory := filepath.Join(pluginDirectory, ".git")
	checkout := &pluginCheckout{
		Plugin:      p,
		CheckoutDir: pluginDirectory,
		HooksDir:    filepath.Join(pluginDirectory, "hooks"),
	}

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
	if e.ExecutorConfig.PluginsAlwaysCloneFresh && utils.FileExists(pluginDirectory) {
		e.shell.Commentf("BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH is true; removing previous checkout of plugin %s", p.Label())
		err = os.RemoveAll(pluginDirectory)
		if err != nil {
			e.shell.Errorf("Oh no, something went wrong removing %s", pluginDirectory)
			return nil, err
		}
	}

	if utils.FileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(ctx, e.shell, pluginDirectory, "--short=7", "HEAD")
		if err != nil {
			e.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			e.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		return checkout, nil
	}

	e.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, pluginDirectory)

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	if e.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, e.shell, repo)
	}

	// Make the directory
	tempDir, err := os.MkdirTemp(e.PluginsPath, id)
	if err != nil {
		return nil, err
	}

	// Switch to the plugin directory
	e.shell.Commentf("Switching to the temporary plugin directory")
	previousWd := e.shell.Getwd()
	if err := e.shell.Chdir(tempDir); err != nil {
		return nil, err
	}
	// Switch back to the previous working directory
	defer func() {
		if err := e.shell.Chdir(previousWd); err != nil && e.Debug {
			e.shell.Errorf("failed to switch back to previous working directory: %v", err)
		}
	}()

	args := []string{"clone", "-v"}
	if e.GitSubmodules {
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
		return e.shell.Run(ctx, "git", args...)
	})
	if err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		e.shell.Commentf("Checking out `%s`", p.Version)
		if err = e.shell.Run(ctx, "git", "checkout", "-f", p.Version); err != nil {
			return nil, err
		}
	}

	e.shell.Commentf("Moving temporary plugin directory to final location")
	err = os.Rename(tempDir, pluginDirectory)
	if err != nil {
		return nil, err
	}

	return checkout, nil
}

// Checkout a given plugin to the plugins directory and return that directory
func (e *Executor) checkoutPlugin(ctx context.Context, p *plugin.Plugin) (*pluginCheckout, error) {
	// Make sure we have a plugin path before trying to do anything
	if e.PluginsPath == "" {
		return nil, fmt.Errorf("Can't checkout plugin without a `plugins-path`")
	}

	id, err := p.Identifier()
	if err != nil {
		return nil, err
	}

	// Ensure the plugin directory exists, otherwise we can't create the lock
	// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
	if err := os.MkdirAll(e.PluginsPath, 0o777); err != nil {
		return nil, err
	}

	// Create a path to the plugin
	pluginDirectory := filepath.Join(e.PluginsPath, id)
	pluginGitDirectory := filepath.Join(pluginDirectory, ".git")
	checkout := &pluginCheckout{
		Plugin:      p,
		CheckoutDir: pluginDirectory,
		HooksDir:    filepath.Join(pluginDirectory, "hooks"),
	}

	// Try and lock this particular plugin while we check it out (we create
	// the file outside of the plugin directory so git clone doesn't have
	// a cry about the directory not being empty)
	pluginCheckoutLock, err := e.shell.LockFile(ctx, filepath.Join(e.PluginsPath, id+".lock"), time.Minute*5)
	if err != nil {
		return nil, err
	}
	defer pluginCheckoutLock.Unlock()

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
	if e.ExecutorConfig.PluginsAlwaysCloneFresh && utils.FileExists(pluginDirectory) {
		e.shell.Commentf("BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH is true; removing previous checkout of plugin %s", p.Label())
		err = os.RemoveAll(pluginDirectory)
		if err != nil {
			e.shell.Errorf("Oh no, something went wrong removing %s", pluginDirectory)
			return nil, err
		}
	}

	if utils.FileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(ctx, e.shell, pluginDirectory, "--short=7", "HEAD")
		if err != nil {
			e.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			e.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		return checkout, nil
	}

	e.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, pluginDirectory)

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	if e.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, e.shell, repo)
	}

	// Make the directory
	tempDir, err := os.MkdirTemp(e.PluginsPath, id)
	if err != nil {
		return nil, err
	}

	// Switch to the plugin directory
	e.shell.Commentf("Switching to the temporary plugin directory")
	previousWd := e.shell.Getwd()
	if err := e.shell.Chdir(tempDir); err != nil {
		return nil, err
	}
	// Switch back to the previous working directory
	defer func() {
		if err := e.shell.Chdir(previousWd); err != nil && e.Debug {
			e.shell.Errorf("failed to switch back to previous working directory: %v", err)
		}
	}()

	args := []string{"clone", "-v"}
	if e.GitSubmodules {
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
		return e.shell.Run(ctx, "git", args...)
	})
	if err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		e.shell.Commentf("Checking out `%s`", p.Version)
		if err = e.shell.Run(ctx, "git", "checkout", "-f", p.Version); err != nil {
			return nil, err
		}
	}

	e.shell.Commentf("Moving temporary plugin directory to final location")
	err = os.Rename(tempDir, pluginDirectory)
	if err != nil {
		return nil, err
	}

	return checkout, nil
}

func (e *Executor) removeCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// on windows, sometimes removing large dirs can fail for various reasons
	// for instance having files open
	// see https://github.com/golang/go/issues/20841
	for i := 0; i < 10; i++ {
		e.shell.Commentf("Removing %s", checkoutPath)
		if err := os.RemoveAll(checkoutPath); err != nil {
			e.shell.Errorf("Failed to remove \"%s\" (%s)", checkoutPath, err)
		} else {
			if _, err := os.Stat(checkoutPath); os.IsNotExist(err) {
				return nil
			} else {
				e.shell.Errorf("Failed to remove %s", checkoutPath)
			}
		}
		e.shell.Commentf("Waiting 10 seconds")
		<-time.After(time.Second * 10)
	}

	return fmt.Errorf("Failed to remove %s", checkoutPath)
}

func (e *Executor) createCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !utils.FileExists(checkoutPath) {
		e.shell.Commentf("Creating \"%s\"", checkoutPath)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(checkoutPath, 0777); err != nil {
			return err
		}
	}

	if e.shell.Getwd() != checkoutPath {
		if err := e.shell.Chdir(checkoutPath); err != nil {
			return err
		}
	}

	return nil
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (e *Executor) CheckoutPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "pre-checkout"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "pre-checkout", e.pluginCheckouts); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if e.CleanCheckout {
		e.shell.Headerf("Cleaning pipeline checkout")
		if err = e.removeCheckoutDir(); err != nil {
			return err
		}
	}

	e.shell.Headerf("Preparing working directory")

	// If we have a blank repository then use a temp dir for builds
	if e.ExecutorConfig.Repository == "" {
		var buildDir string
		buildDir, err = os.MkdirTemp("", "buildkite-job-"+e.ExecutorConfig.JobID)
		if err != nil {
			return err
		}
		e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", buildDir)

		// Track the directory so we can remove it at the end of the job
		e.cleanupDirs = append(e.cleanupDirs, buildDir)
	}

	// Make sure the build directory exists
	if err := e.createCheckoutDir(); err != nil {
		return err
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case e.hasPluginHook("checkout"):
		if err := e.executePluginHook(ctx, "checkout", e.pluginCheckouts); err != nil {
			return err
		}
	case e.hasGlobalHook("checkout"):
		if err := e.executeGlobalHook(ctx, "checkout"); err != nil {
			return err
		}
	default:
		if e.ExecutorConfig.Repository == "" {
			e.shell.Commentf("Skipping checkout, BUILDKITE_REPO is empty")
			break
		}

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(3),
			roko.WithStrategy(roko.Constant(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			err := e.defaultCheckoutPhase(ctx)
			if err == nil {
				return nil
			}

			switch {
			case shell.IsExitError(err) && shell.GetExitCode(err) == -1:
				e.shell.Warningf("Checkout was interrupted by a signal")
				r.Break()

			case errors.Is(err, context.Canceled):
				e.shell.Warningf("Checkout was cancelled")
				r.Break()

			case errors.Is(ctx.Err(), context.Canceled):
				e.shell.Warningf("Checkout was cancelled due to context cancellation")
				r.Break()

			default:
				e.shell.Warningf("Checkout failed! %s (%s)", err, r)

				// Specifically handle git errors
				if ge := new(gitError); errors.As(err, &ge) {
					switch ge.Type {
					// These types can fail because of corrupted checkouts
					case gitErrorClean, gitErrorCleanSubmodules, gitErrorClone,
						gitErrorCheckoutRetryClean, gitErrorFetchRetryClean,
						gitErrorFetchBadObject:
					// Otherwise, don't clean the checkout dir
					default:
						return err
					}
				}

				// Checkout can fail because of corrupted files in the checkout
				// which can leave the agent in a state where it keeps failing
				// This removes the checkout dir, which means the next checkout
				// will be a lot slower (clone vs fetch), but hopefully will
				// allow the agent to self-heal
				if err := e.removeCheckoutDir(); err != nil {
					e.shell.Printf("Failed to remove checkout dir while cleaning up after a checkout error.")
				}

				// Now make sure the build directory exists again before we try
				// to checkout again, or proceed and run hooks which presume the
				// checkout dir exists
				if err := e.createCheckoutDir(); err != nil {
					return err
				}
			}

			return err
		}); err != nil {
			return err
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath, exists := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	if !exists {
		e.shell.Printf("Could not determine previous checkout path from BUILDKITE_BUILD_CHECKOUT_PATH")
	}

	// Run post-checkout hooks
	if err := e.executeGlobalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err := e.executeLocalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err := e.executePluginHook(ctx, "post-checkout", e.pluginCheckouts); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		e.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err := e.shell.Chdir(newCheckoutPath); err != nil {
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

func (e *Executor) updateGitMirror(ctx context.Context, repository string) (string, error) {
	// Create a unique directory for the repository mirror
	mirrorDir := filepath.Join(e.ExecutorConfig.GitMirrorsPath, dirForRepository(repository))
	isMainRepository := repository == e.Repository

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !utils.FileExists(baseDir) {
		e.shell.Commentf("Creating \"%s\"", baseDir)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(baseDir, 0777); err != nil {
			return "", err
		}
	}

	e.shell.Chdir(e.ExecutorConfig.GitMirrorsPath)

	lockTimeout := time.Second * time.Duration(e.GitMirrorsLockTimeout)

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository clone lock")
	}

	// Lock the mirror dir to prevent concurrent clones
	mirrorCloneLock, err := e.shell.LockFile(ctx, mirrorDir+".clonelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorCloneLock.Unlock()

	// If we don't have a mirror, we need to clone it
	if !utils.FileExists(mirrorDir) {
		e.shell.Commentf("Cloning a mirror of the repository to %q", mirrorDir)
		flags := "--mirror " + e.GitCloneMirrorFlags
		if err := gitClone(ctx, e.shell, flags, repository, mirrorDir); err != nil {
			e.shell.Commentf("Removing mirror dir %q due to failed clone", mirrorDir)
			if err := os.RemoveAll(mirrorDir); err != nil {
				e.shell.Errorf("Failed to remove \"%s\" (%s)", mirrorDir, err)
			}
			return "", err
		}

		return mirrorDir, nil
	}

	// If it exists, immediately release the clone lock
	mirrorCloneLock.Unlock()

	// Check if the mirror has a commit, this is atomic so should be safe to do
	if isMainRepository {
		if hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) {
			e.shell.Commentf("Commit %q exists in mirror", e.Commit)
			return mirrorDir, nil
		}
	}

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository update lock")
	}

	// Lock the mirror dir to prevent concurrent updates
	mirrorUpdateLock, err := e.shell.LockFile(ctx, mirrorDir+".updatelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorUpdateLock.Unlock()

	if isMainRepository {
		// Check again after we get a lock, in case the other process has already updated
		if hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) {
			e.shell.Commentf("Commit %q exists in mirror", e.Commit)
			return mirrorDir, nil
		}
	}

	e.shell.Commentf("Updating existing repository mirror to find commit %s", e.Commit)

	// Update the origin of the repository so we can gracefully handle
	// repository renames.
	urlChanged, err := e.updateRemoteURL(ctx, mirrorDir, repository)
	if err != nil {
		return "", fmt.Errorf("setting remote URL: %w", err)
	}

	if isMainRepository {
		if e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github") {
			e.shell.Commentf("Fetch and mirror pull request head from GitHub")
			refspec := fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
			// Fetch the PR head from the upstream repository into the mirror.
			if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin", refspec); err != nil {
				return "", err
			}
		} else {
			// Fetch the build branch from the upstream repository into the mirror.
			if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin", e.Branch); err != nil {
				return "", err
			}
		}
	} else { // not the main repo.

		// This is a mirror of a submodule.
		// Update without specifying particular ref, since we don't know which
		// ref is needed for the main build.
		// (If it doesn't contain the needed ref, then the build would fail on
		// a clean host or with a clean checkout.)
		// TODO: Investigate getting the ref from the main repo and passing
		// that in here.
		if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin"); err != nil {
			return "", err
		}
	}

	if urlChanged {
		// Let's opportunistically fsck and gc.
		// 1. In case of remote URL confusion (bug introduced in #1959), and
		// 2. There's possibly some object churn when remotes are renamed.
		if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fsck"); err != nil {
			e.shell.Logger.Warningf("Couldn't run git fsck: %v", err)
		}
		if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "gc"); err != nil {
			e.shell.Logger.Warningf("Couldn't run git gc: %v", err)
		}
	}

	return mirrorDir, nil
}

// updateRemoteURL updates the URL for 'origin'. If gitDir == "", it assumes the
// local repo is in the current directory, otherwise it includes --git-dir.
// If the remote has changed, it logs some extra information. updateRemoteURL
// reports if the remote URL changed.
func (e *Executor) updateRemoteURL(ctx context.Context, gitDir, repository string) (bool, error) {
	// Update the origin of the repository so we can gracefully handle
	// repository renames.

	// First check what the existing remote is, for both logging and debugging
	// purposes.
	args := []string{"remote", "get-url", "origin"}
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	gotURL, err := e.shell.RunAndCapture(ctx, "git", args...)
	if err != nil {
		return false, err
	}

	if gotURL == repository {
		// No need to update anything
		return false, nil
	}

	gd := gitDir
	if gd == "" {
		gd = e.shell.Getwd()
	}

	e.shell.Commentf("Remote URL for git directory %s has changed (%s -> %s)!", gd, gotURL, repository)
	e.shell.Commentf("This is usually because the repository has been renamed.")
	e.shell.Commentf("If this is unexpected, you may see failures.")

	args = []string{"remote", "set-url", "origin", repository}
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	return true, e.shell.Run(ctx, "git", args...)
}

func (e *Executor) getOrUpdateMirrorDir(ctx context.Context, repository string) (string, error) {
	var mirrorDir string
	// Skip updating the Git mirror before using it?
	if e.ExecutorConfig.GitMirrorsSkipUpdate {
		mirrorDir = filepath.Join(e.ExecutorConfig.GitMirrorsPath, dirForRepository(repository))
		e.shell.Commentf("Skipping update and using existing mirror for repository %s at %s.", repository, mirrorDir)

		// Check if specified mirrorDir exists, otherwise the clone will fail.
		if !utils.FileExists(mirrorDir) {
			// Fall back to a clean clone, rather than failing the clone and therefore the build
			e.shell.Commentf("No existing mirror found for repository %s at %s.", repository, mirrorDir)
			mirrorDir = ""
		}
		return mirrorDir, nil
	}

	return e.updateGitMirror(ctx, repository)
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (e *Executor) defaultCheckoutPhase(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "repo-checkout", e.ExecutorConfig.TracingBackend)
	span.AddAttributes(map[string]string{
		"checkout.repo_name": e.Repository,
		"checkout.refspec":   e.RefSpec,
		"checkout.commit":    e.Commit,
	})
	var err error
	defer func() { span.FinishWithError(err) }()

	if e.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, e.shell, e.Repository)
	}

	var mirrorDir string

	// If we can, get a mirror of the git repository to use for reference later
	if e.ExecutorConfig.GitMirrorsPath != "" && e.ExecutorConfig.Repository != "" {
		span.AddAttributes(map[string]string{"checkout.is_using_git_mirrors": "true"})
		mirrorDir, err = e.getOrUpdateMirrorDir(ctx, e.Repository)
		if err != nil {
			return fmt.Errorf("getting/updating git mirror: %w", err)
		}

		e.shell.Env.Set("BUILDKITE_REPO_MIRROR", mirrorDir)
	}

	// Make sure the build directory exists and that we change directory into it
	if err := e.createCheckoutDir(); err != nil {
		return fmt.Errorf("creating checkout dir: %w", err)
	}

	gitCloneFlags := e.GitCloneFlags
	if mirrorDir != "" {
		gitCloneFlags += fmt.Sprintf(" --reference %q", mirrorDir)
	}

	// Does the git directory exist?
	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if utils.FileExists(existingGitDir) {
		// Update the origin of the repository so we can gracefully handle
		// repository renames
		if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
			return fmt.Errorf("setting origin: %w", err)
		}
	} else {
		if err := gitClone(ctx, e.shell, gitCloneFlags, e.Repository, "."); err != nil {
			return fmt.Errorf("cloning git repository: %w", err)
		}
	}

	// Git clean prior to checkout, we do this even if submodules have been
	// disabled to ensure previous submodules are cleaned up
	if hasGitSubmodules(e.shell) {
		if err := gitCleanSubmodules(ctx, e.shell, e.GitCleanFlags); err != nil {
			return fmt.Errorf("cleaning git submodules: %w", err)
		}
	}

	if err := gitClean(ctx, e.shell, e.GitCleanFlags); err != nil {
		return fmt.Errorf("cleaning git repository: %w", err)
	}

	gitFetchFlags := e.GitFetchFlags

	switch {
	case e.RefSpec != "":
		// If a refspec is provided then use it instead.
		// For example, `refs/not/a/head`
		e.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", e.RefSpec); err != nil {
			return fmt.Errorf("fetching refspec %q: %w", e.RefSpec, err)
		}

	case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not there is a current head in this repository or another which
		// references the commit. We presume a commit sha is provided. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
		e.shell.Commentf("Fetch and checkout pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", e.PullRequest)

		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", refspec); err != nil {
			return fmt.Errorf("fetching PR refspec %q: %w", refspec, err)
		}

		gitFetchHead, _ := e.shell.RunAndCapture(ctx, "git", "rev-parse", "FETCH_HEAD")
		e.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

	case e.Commit == "HEAD":
		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
		e.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", e.Branch); err != nil {
			return fmt.Errorf("fetching branch %q: %w", e.Branch, err)
		}

	default:
		// Otherwise fetch and checkout the commit directly.
		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", e.Commit); err == nil {
			break // it worked, break out of the switch statement
		} else if gerr := new(gitError); errors.As(err, &gerr) {
			// if we fail in a way that means the repository is corrupt, we should bail
			switch gerr.Type {
			case gitErrorFetchRetryClean, gitErrorFetchBadObject:
				return fmt.Errorf("fetching commit %q: %w", e.Commit, err)
			}
		}

		// Some repositories don't support fetching a specific commit so we fall
		// back to fetching all heads and tags, hoping that the commit is included.
		e.shell.Commentf("Commit fetch failed, trying to fetch all heads and tags")
		// By default `git fetch origin` will only fetch tags which are
		// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
		// fetch all tags in addition to the default refspec, but pre 1.9.0 it
		// excludes the default refspec.
		gitFetchRefspec, err := e.shell.RunAndCapture(ctx, "git", "config", "remote.origin.fetch")
		if err != nil {
			return fmt.Errorf("getting remote.origin.fetch: %w", err)
		}

		if err := gitFetch(ctx, e.shell,
			gitFetchFlags, "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*",
		); err != nil {
			return fmt.Errorf("fetching commit %q: %w", e.Commit, err)
		}
	}

	gitCheckoutFlags := e.GitCheckoutFlags

	if e.Commit == "HEAD" {
		if err := gitCheckout(ctx, e.shell, gitCheckoutFlags, "FETCH_HEAD"); err != nil {
			return fmt.Errorf("checking out FETCH_HEAD: %w", err)
		}
	} else {
		if err := gitCheckout(ctx, e.shell, gitCheckoutFlags, e.Commit); err != nil {
			return fmt.Errorf("checking out commit %q: %w", e.Commit, err)
		}
	}

	gitSubmodules := false
	if hasGitSubmodules(e.shell) {
		if e.GitSubmodules {
			e.shell.Commentf("Git submodules detected")
			gitSubmodules = true
		} else {
			e.shell.Warningf("This repository has submodules, but submodules are disabled at an agent level")
		}
	}

	if gitSubmodules {
		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the job
		// script, and show an informative error.
		if err := e.shell.Run(ctx, "git", "submodule", "sync", "--recursive"); err != nil {
			gitVersionOutput, _ := e.shell.RunAndCapture(ctx, "git", "--version")
			e.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		args := []string{}
		for _, config := range e.GitSubmoduleCloneConfig {
			args = append(args, "-c", config)
		}

		// Checking for submodule repositories
		submoduleRepos, err := gitEnumerateSubmoduleURLs(ctx, e.shell)
		if err != nil {
			e.shell.Warningf("Failed to enumerate git submodules: %v", err)
		} else {
			mirrorSubmodules := e.ExecutorConfig.GitMirrorsPath != ""
			for _, repository := range submoduleRepos {
				submoduleArgs := append([]string(nil), args...)
				// submodules might need their fingerprints verified too
				if e.SSHKeyscan {
					addRepositoryHostToSSHKnownHosts(ctx, e.shell, repository)
				}

				if !mirrorSubmodules {
					continue
				}
				// It's all mirrored submodules for the rest of the loop.

				mirrorDir, err := e.getOrUpdateMirrorDir(ctx, repository)
				if err != nil {
					return fmt.Errorf("getting/updating mirror dir for submodules: %w", err)
				}

				// Switch back to the checkout dir, doing other operations from GitMirrorsPath will fail.
				if err := e.createCheckoutDir(); err != nil {
					return fmt.Errorf("creating checkout dir: %w", err)
				}

				// Tests use a local temp path for the repository, real repositories don't. Handle both.
				var repositoryPath string
				if !utils.FileExists(repository) {
					repositoryPath = filepath.Join(e.ExecutorConfig.GitMirrorsPath, dirForRepository(repository))
				} else {
					repositoryPath = repository
				}

				if mirrorDir != "" {
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force", "--reference", repositoryPath)
				} else {
					// Fall back to a clean update, rather than failing the checkout and therefore the build
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force")
				}

				if err := e.shell.Run(ctx, "git", submoduleArgs...); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			if !mirrorSubmodules {
				args = append(args, "submodule", "update", "--init", "--recursive", "--force")
				if err := e.shell.Run(ctx, "git", args...); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			if err := e.shell.Run(ctx, "git", "submodule", "foreach", "--recursive", "git reset --hard"); err != nil {
				return fmt.Errorf("resetting submodules: %w", err)
			}
		}
	}

	// Git clean after checkout. We need to do this because submodules could have
	// changed in between the last checkout and this one. A double clean is the only
	// good solution to this problem that we've found
	e.shell.Commentf("Cleaning again to catch any post-checkout changes")

	if err := gitClean(ctx, e.shell, e.GitCleanFlags); err != nil {
		return fmt.Errorf("cleaning repository post-checkout: %w", err)
	}

	if gitSubmodules {
		if err := gitCleanSubmodules(ctx, e.shell, e.GitCleanFlags); err != nil {
			return fmt.Errorf("cleaning submodules post-checkout: %w", err)
		}
	}

	if _, hasToken := e.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN"); !hasToken {
		e.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// resolve BUILDKITE_COMMIT based on the local git repo
	if experiments.IsEnabled(experiments.ResolveCommitAfterCheckout) {
		e.shell.Commentf("Using resolve-commit-after-checkout experiment 🧪")
		e.resolveCommit(ctx)
	}

	// Grab author and commit information and send it back to Buildkite. But before we do, we'll check
	// to see if someone else has done it first.
	e.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if err := e.shell.Run(ctx, "buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
		e.shell.Commentf("Sending Git commit information back to Buildkite")
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
		out, err := e.shell.RunAndCapture(ctx, "git", gitArgs...)
		if err != nil {
			return fmt.Errorf("getting git commit information: %w", err)
		}
		stdin := strings.NewReader(out)
		if err := e.shell.WithStdin(stdin).Run(ctx, "buildkite-agent", "meta-data", "set", "buildkite:git:commit"); err != nil {
			return fmt.Errorf("sending git commit information to Buildkite: %w", err)
		}
	}

	return nil
}

func (e *Executor) resolveCommit(ctx context.Context) {
	commitRef, _ := e.shell.Env.Get("BUILDKITE_COMMIT")
	if commitRef == "" {
		e.shell.Warningf("BUILDKITE_COMMIT was empty")
		return
	}
	cmdOut, err := e.shell.RunAndCapture(ctx, "git", "rev-parse", commitRef)
	if err != nil {
		e.shell.Warningf("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	if trimmedCmdOut != commitRef {
		e.shell.Commentf("Updating BUILDKITE_COMMIT from %q to %q", commitRef, trimmedCmdOut)
		e.shell.Env.Set("BUILDKITE_COMMIT", trimmedCmdOut)
	}
}

// runPreCommandHooks runs the pre-command hooks and adds tracing spans.
func (e *Executor) runPreCommandHooks(ctx context.Context) error {
	spanName := e.implementationSpecificSpanName("pre-command", "pre-command hooks")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "pre-command"); err != nil {
		return err
	}
	if err = e.executeLocalHook(ctx, "pre-command"); err != nil {
		return err
	}
	if err = e.executePluginHook(ctx, "pre-command", e.pluginCheckouts); err != nil {
		return err
	}
	return nil
}

// runCommand runs the command and adds tracing spans.
func (e *Executor) runCommand(ctx context.Context) error {
	var err error
	// There can only be one command hook, so we check them in order of plugin, local
	switch {
	case e.hasPluginHook("command"):
		err = e.executePluginHook(ctx, "command", e.pluginCheckouts)
	case e.hasLocalHook("command"):
		err = e.executeLocalHook(ctx, "command")
	case e.hasGlobalHook("command"):
		err = e.executeGlobalHook(ctx, "command")
	default:
		err = e.defaultCommandPhase(ctx)
	}
	return err
}

// runPostCommandHooks runs the post-command hooks and adds tracing spans.
func (e *Executor) runPostCommandHooks(ctx context.Context) error {
	spanName := e.implementationSpecificSpanName("post-command", "post-command hooks")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "post-command"); err != nil {
		return err
	}
	if err = e.executeLocalHook(ctx, "post-command"); err != nil {
		return err
	}
	if err = e.executePluginHook(ctx, "post-command", e.pluginCheckouts); err != nil {
		return err
	}
	return nil
}

// CommandPhase determines how to run the build, and then runs it
func (e *Executor) CommandPhase(ctx context.Context) (hookErr error, commandErr error) {
	var preCommandErr error

	span, ctx := tracetools.StartSpanFromContext(ctx, "command", e.ExecutorConfig.TracingBackend)
	defer func() {
		span.FinishWithError(hookErr)
	}()

	// Run postCommandHooks, even if there is an error from the command, but not if there is an
	// error from the pre-command hooks. Note: any post-command hook error will be returned.
	defer func() {
		if preCommandErr != nil {
			return
		}
		hookErr = e.runPostCommandHooks(ctx)
	}()

	// Run pre-command hooks
	if preCommandErr = e.runPreCommandHooks(ctx); preCommandErr != nil {
		return preCommandErr, nil
	}

	// Run the command
	commandErr = e.runCommand(ctx)

	// Save the command exit status to the env so hooks + plugins can access it. If there is no
	// error this will be zero. It's used to set the exit code later, so it's important
	e.shell.Env.Set(
		"BUILDKITE_COMMAND_EXIT_STATUS",
		fmt.Sprintf("%d", shell.GetExitCode(commandErr)),
	)

	// Exit early if there was no error
	if commandErr == nil {
		return nil, nil
	}

	// Expand the job log header from the command to surface the error
	e.shell.Printf("^^^ +++")

	isExitError := shell.IsExitError(commandErr)
	isExitSignaled := shell.IsExitSignaled(commandErr)
	avoidRecursiveTrap := experiments.IsEnabled(experiments.AvoidRecursiveTrap)

	switch {
	case isExitError && isExitSignaled && avoidRecursiveTrap:
		// The recursive trap created a segfault that we were previously inadvertently suppressing
		// in the next branch. Once the experiment is promoted, we should keep this branch in case
		// to show the error to users.
		e.shell.Errorf("The command was interrupted by a signal: %v", commandErr)

		// although error is an exit error, it's not returned. (seems like a bug)
		// TODO: investigate phasing this out under a experiment
		return nil, nil
	case isExitError && isExitSignaled && !avoidRecursiveTrap:
		// TODO: remove this branch when the experiment is promoted
		e.shell.Errorf("The command was interrupted by a signal")

		// although error is an exit error, it's not returned. (seems like a bug)
		return nil, nil
	case isExitError && !isExitSignaled:
		e.shell.Errorf("The command exited with status %d", shell.GetExitCode(commandErr))
		return nil, commandErr
	default:
		e.shell.Errorf("%s", commandErr)

		// error is not an exit error, we don't want to return it
		return nil, nil
	}
}

// defaultCommandPhase is executed if there is no global or plugin command hook
func (e *Executor) defaultCommandPhase(ctx context.Context) error {
	spanName := e.implementationSpecificSpanName("default command hook", "hook.execute")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()
	span.AddAttributes(map[string]string{
		"hook.name": "command",
		"hook.type": "default",
	})

	// Make sure we actually have a command to run
	if strings.TrimSpace(e.Command) == "" {
		return fmt.Errorf("The command phase has no `command` to execute. Provide a `command` field in your step configuration, or define a `command` hook in a step plug-in, your repository `.buildkite/hooks`, or agent `hooks-path`.")
	}

	scriptFileName := strings.Replace(e.Command, "\n", "", -1)
	pathToCommand, err := filepath.Abs(filepath.Join(e.shell.Getwd(), scriptFileName))
	commandIsScript := err == nil && utils.FileExists(pathToCommand)
	span.AddAttributes(map[string]string{"hook.command": pathToCommand})

	// If the command isn't a script, then it's something we need
	// to eval. But before we even try running it, we should double
	// check that the agent is allowed to eval commands.
	if !commandIsScript && !e.CommandEval {
		e.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	// Also make sure that the script we've resolved is definitely within this
	// repository checkout and isn't elsewhere on the system.
	if commandIsScript && !e.CommandEval && !strings.HasPrefix(pathToCommand, e.shell.Getwd()+string(os.PathSeparator)) {
		e.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is only allowed to run scripts within your repository. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	var cmdToExec string

	// The shell gets parsed based on the operating system
	shell, err := shellwords.Split(e.Shell)
	if err != nil {
		return fmt.Errorf("Failed to split shell (%q) into tokens: %v", e.Shell, err)
	}

	if len(shell) == 0 {
		return fmt.Errorf("No shell set for job")
	}

	// Windows CMD.EXE is horrible and can't handle newline delimited commands. We write
	// a batch script so that it works, but we don't like it
	if strings.ToUpper(filepath.Base(shell[0])) == "CMD.EXE" {
		batchScript, err := e.writeBatchScript(e.Command)
		if err != nil {
			return err
		}
		defer os.Remove(batchScript)

		e.shell.Headerf("Running batch script")
		if e.Debug {
			contents, err := os.ReadFile(batchScript)
			if err != nil {
				return err
			}
			e.shell.Commentf("Wrote batch script %s\n%s", batchScript, contents)
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
		if e.ExecutorConfig.CommandEval {
			// Make script executable
			if err = utils.ChmodExecutable(pathToCommand); err != nil {
				e.shell.Warningf("Error marking script %q as executable: %v", pathToCommand, err)
				return err
			}
		}

		// Make the path relative to the shell working dir
		scriptPath, err := filepath.Rel(e.shell.Getwd(), pathToCommand)
		if err != nil {
			return err
		}

		e.shell.Headerf("Running script")
		cmdToExec = fmt.Sprintf(".%c%s", os.PathSeparator, scriptPath)
	} else {
		e.shell.Headerf("Running commands")
		cmdToExec = e.Command
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(e.shell) {
		if e.Debug {
			e.shell.Commentf("Detected deprecated docker environment variables")
		}
		err = runDeprecatedDockerIntegration(ctx, e.shell, []string{cmdToExec})
		return err
	}

	// We added the `trap` below because we used to think that:
	//
	// If we aren't running a script, try and detect if we are using a posix shell
	// and if so add a trap so that the intermediate shell doesn't swallow signals
	// from cancellation
	//
	// But on further investigation:
	// 1. The trap is recursive and leads to an infinite loop of signals and a segfault.
	// 2. It just signals the intermediate shell again. If the intermediate shell swallowed signals
	//    in the first place then signaling it again won't change that.
	// 3. Propogating signals to child processes is handled by signalling their process group
	//    elsewhere in the agent.
	//
	// Therefore, we are phasing it out under an experiment.
	if !experiments.IsEnabled(experiments.AvoidRecursiveTrap) && !commandIsScript && shellscript.IsPOSIXShell(e.Shell) {
		cmdToExec = fmt.Sprintf("trap 'kill -- $$' INT TERM QUIT; %s", cmdToExec)
	}

	redactors := e.setupRedactors()
	defer redactors.Flush()

	var cmd []string
	cmd = append(cmd, shell...)
	cmd = append(cmd, cmdToExec)

	if e.Debug {
		e.shell.Promptf("%s", process.FormatCommand(cmd[0], cmd[1:]))
	} else {
		e.shell.Promptf("%s", cmdToExec)
	}

	err = e.shell.RunWithoutPrompt(ctx, cmd[0], cmd[1:]...)
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

func (e *Executor) writeBatchScript(cmd string) (string, error) {
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

func (e *Executor) artifactPhase(ctx context.Context) error {
	if e.AutomaticArtifactUploadPaths == "" {
		return nil
	}

	spanName := e.implementationSpecificSpanName("artifacts", "artifact upload")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	err = e.preArtifactHooks(ctx)
	if err != nil {
		return err
	}

	err = e.uploadArtifacts(ctx)
	if err != nil {
		return err
	}

	err = e.postArtifactHooks(ctx)
	if err != nil {
		return err
	}

	return nil
}

// Run the pre-artifact hooks
func (e *Executor) preArtifactHooks(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-artifact", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = e.executeLocalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "pre-artifact", e.pluginCheckouts); err != nil {
		return err
	}

	return nil
}

// Run the artifact upload command
func (e *Executor) uploadArtifacts(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "artifact-upload", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	e.shell.Headerf("Uploading artifacts")
	args := []string{"artifact", "upload", e.AutomaticArtifactUploadPaths}

	// If blank, the upload destination is buildkite
	if e.ArtifactUploadDestination != "" {
		args = append(args, e.ArtifactUploadDestination)
	}

	if err = e.shell.Run(ctx, "buildkite-agent", args...); err != nil {
		return err
	}

	return nil
}

// Run the post-artifact hooks
func (e *Executor) postArtifactHooks(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "post-artifact", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = e.executeLocalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "post-artifact", e.pluginCheckouts); err != nil {
		return err
	}

	return nil
}

// Check for ignored env variables from the job runner. Some
// env (for example, BUILDKITE_BUILD_PATH) can only be set from config or by hooks.
// If these env are set at a pipeline level, we rewrite them to BUILDKITE_X_BUILD_PATH
// and warn on them here so that users know what is going on
func (e *Executor) ignoredEnv() []string {
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
// redactor.Mux (possibly empty) is returned so the caller can `defer redactor.Flush()`
func (e *Executor) setupRedactors() replacer.Mux {
	valuesToRedact := redact.Values(e.shell, e.ExecutorConfig.RedactedVars, e.shell.Env.Dump())
	if len(valuesToRedact) == 0 {
		return nil
	}

	if e.Debug {
		e.shell.Commentf("Enabling output redaction for values from environment variables matching: %v", e.ExecutorConfig.RedactedVars)
	}

	var mux replacer.Mux

	// If the shell Writer is already a Replacer, reset the values to redact.
	if rdc, ok := e.shell.Writer.(*replacer.Replacer); ok {
		rdc.Reset(valuesToRedact)
		mux = append(mux, rdc)
	} else {
		rdc := replacer.New(e.shell.Writer, valuesToRedact, redact.Redact)
		e.shell.Writer = rdc
		mux = append(mux, rdc)
	}

	// If the shell.Logger is already a redacted WriterLogger, reset the values to redact.
	// (maybe there's a better way to do two levels of type assertion? ...
	// shell.Logger may be a WriterLogger, and its Writer may be a Redactor)
	var shellWriterLogger *shell.WriterLogger
	var shellLoggerRedactor *replacer.Replacer
	if logger, ok := e.shell.Logger.(*shell.WriterLogger); ok {
		shellWriterLogger = logger
		if redactor, ok := logger.Writer.(*replacer.Replacer); ok {
			shellLoggerRedactor = redactor
		}
	}
	if rdc := shellLoggerRedactor; rdc != nil {
		rdc.Reset(valuesToRedact)
		mux = append(mux, rdc)
	} else if shellWriterLogger != nil {
		rdc := replacer.New(e.shell.Writer, valuesToRedact, redact.Redact)
		shellWriterLogger.Writer = rdc
		mux = append(mux, rdc)
	}

	return mux
}

type pluginCheckout struct {
	*plugin.Plugin
	*plugin.Definition
	CheckoutDir string
	HooksDir    string
}

func (e *Executor) startKubernetesClient(ctx context.Context, kubernetesClient *kubernetes.Client) error {
	e.shell.Commentf("Using experimental Kubernetes support")
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
		e.shell.Env.Set("BUILDKITE_AGENT_ACCESS_TOKEN", connect.AccessToken)
		writer := io.MultiWriter(os.Stdout, kubernetesClient)
		e.shell.Writer = writer
		e.shell.Logger = &shell.WriterLogger{
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
			e.shell.Errorf("Error waiting for client interrupt: %v", err)
		}
		e.cancelCh <- struct{}{}
	}()
	return nil
}
