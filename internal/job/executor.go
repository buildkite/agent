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

	includePhase := func(phase string) bool {
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
	if herr := new(hook.HookExitError); errors.As(err, &herr) {
		// Ignore changes to env when there was a hook exited with an error, but continue
		if e.shell.Debug {
			e.shell.Commentf("Hook exited with error: %v, ignoring environment changes", herr)
		}
		return nil
	} else if err != nil {
		// Fail if there were any errors other than the hook exiting with a non-zero exit code
		return fmt.Errorf("Failed to get environment: %w", err)
	}

	// Hook exited successfully (and not early!) We have an environment and
	// wd change we can apply to our subsequent phases
	e.applyEnvironmentChanges(changes, redactors)

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

	scriptContents := []string{"@echo off"}

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
