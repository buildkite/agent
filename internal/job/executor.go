// package job provides management of the phases of execution of a
// Buildkite job.
//
// It is intended for internal use by buildkite-agent only.
package job

import (
	"context"
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
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/file"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/shellscript"
	"github.com/buildkite/agent/v3/internal/tempfile"
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
	if experiments.IsEnabled(ctx, experiments.KubernetesExec) {
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
	cleanup, err := e.startJobAPI(ctx)
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

	if !experiments.IsEnabled(ctx, experiments.PolyglotHooks) {
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

func logOpenedHookInfo(l shell.Logger, debug bool, hookName, path string) {
	switch {
	case runtime.GOOS == "linux":
		procPath, err := file.OpenedBy(l, debug, path)
		if err != nil {
			l.Errorf("The %s hook failed to run because it was already open. We couldn't find out what process had the hook open", hookName)

		} else {
			l.Errorf("The %s hook failed to run the %s process has the hook file open", hookName, procPath)
		}
	case utils.FileExists("/dev/fd"):
		isOpened, err := file.IsOpened(l, debug, path)
		if err == nil {
			if isOpened {
				l.Errorf("The %s hook failed to run because it was opened by this buildkite-agent")
			} else {
				l.Errorf("The %s hook failed to run because it was opened by another process")
			}
			break
		}
		fallthrough
	default:
		l.Errorf("The %s hook failed to run because it was opened", hookName)
	}
}

func (e *Executor) runWrappedShellScriptHook(ctx context.Context, hookName string, hookCfg HookConfig) error {
	redactors := e.setupRedactors()
	defer redactors.Flush()

	script, err := hook.NewWrapper(hook.WithHookPath(hookCfg.Path))
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
		e.shell.Commentf("A hook runner was written to %q with the following:", script.Path())
		e.shell.Promptf("%s", process.FormatCommand(script.Path(), nil))
	} else {
		e.shell.Promptf("%s", process.FormatCommand(cleanHookPath, []string{}))
	}

	const maxHookRetry = 3

	// Run the wrapper script
	if err := roko.NewRetrier(
		roko.WithStrategy(roko.Constant(time.Second)),
		roko.WithMaxAttempts(maxHookRetry),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Run the script and only retry on fork/exec errors
		err := e.shell.RunScript(ctx, script.Path(), hookCfg.Env)
		if perr := new(os.PathError); errors.As(err, &perr) && perr.Op == "fork/exec" {
			return err
		}
		r.Break()
		return err
	}); err != nil {
		exitCode := shell.GetExitCode(err)
		e.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", exitCode))

		// If the hook exited with a non-zero exit code, then we should pass that back to the executor
		// so it may inform the Buildkite API
		if shell.IsExitError(err) {
			return &shell.ExitError{
				Code:    exitCode,
				Message: fmt.Sprintf("The %s hook exited with status %d", hookName, exitCode),
			}
		}

		// If the error is from fork/exec, then inspect the file to see why it failed to be executed,
		// even after the retry
		if perr := new(os.PathError); errors.As(err, &perr) && perr.Op == "fork/exec" {
			logOpenedHookInfo(e.shell.Logger, e.Debug, hookName, perr.Path)
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
		case *hook.ExitError:
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

func (e *Executor) applyEnvironmentChanges(changes hook.EnvChanges, redactors replacer.Mux) {
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
	avoidRecursiveTrap := experiments.IsEnabled(ctx, experiments.AvoidRecursiveTrap)

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
	if !experiments.IsEnabled(ctx, experiments.AvoidRecursiveTrap) && !commandIsScript && shellscript.IsPOSIXShell(e.Shell) {
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
	scriptFile, err := tempfile.New(tempfile.WithName("buildkite-script.bat"), tempfile.KeepingExtension())
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
