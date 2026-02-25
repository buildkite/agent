// package job provides management of the phases of execution of a
// Buildkite job.
//
// It is intended for internal use by buildkite-agent only.
package job

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/file"
	"github.com/buildkite/agent/v3/internal/job/hook"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/secrets"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/internal/shellscript"
	"github.com/buildkite/agent/v3/internal/tempfile"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/go-pipeline"
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

	// The checkout directory root
	checkoutRoot *os.Root

	// Plugins to use
	plugins []*plugin.Plugin

	// Plugin checkouts from the plugin phases
	pluginCheckouts []*pluginCheckout

	// Directories to clean up at end of job execution
	cleanupDirs []string

	// A channel to track cancellation
	cancelMu  sync.Mutex
	cancelCh  chan struct{}
	cancelled bool

	// redactors for the job logs. The will be populated with values both from environment variable and through the Job API.
	// In order for the latter to happen, a reference is passed into the the Job API server as well
	redactors *replacer.Mux
}

// New returns a new executor instance
func New(conf ExecutorConfig) *Executor {
	return &Executor{
		ExecutorConfig: conf,
		cancelCh:       make(chan struct{}),
		redactors:      replacer.NewMux(),
	}
}

// Run the job and return the exit code
func (e *Executor) Run(ctx context.Context) (exitCode int) {
	// Create a context to use for cancelation of the job
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start with stdout and stderr as their usual selves.
	stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)

	// The shell environment is initially the current environment, needed for setupRedactors.
	environ := env.FromSlice(os.Environ())

	// Create a logger to stderr that can be used for things prior to the
	// redactor setup.
	// Be careful not to log customer secrets here!
	tempLog := shell.NewWriterLogger(stderr, true, e.DisabledWarnings)

	// setup the redactors here once and for the life of the executor
	// they will be flushed at the end of each hook
	preRedactedStdout, preRedactedLogger := e.setupRedactors(tempLog, environ, stdout, stderr)

	// Check if not nil to allow for tests to overwrite shell.
	if e.shell == nil {
		sh, err := shell.New(
			shell.WithDebug(e.Debug),
			shell.WithEnv(environ),
			shell.WithLogger(preRedactedLogger), // shell -> logger -> redactor -> real stderr
			shell.WithInterruptSignal(e.CancelSignal),
			shell.WithPTY(e.RunInPty),
			shell.WithStdout(preRedactedStdout), // shell -> redactor -> real stdout
			shell.WithSignalGracePeriod(e.SignalGracePeriod),
			shell.WithTraceContextCodec(e.TraceContextCodec),
		)
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			return 1
		}
		e.shell = sh
	}

	var err error
	span, ctx, stopper := e.startTracing(ctx)
	defer stopper()
	defer func() { span.FinishWithError(err) }()

	// Listen for cancellation. Once ctx is cancelled, some tasks can run
	// afterwards during the signal grace period. These use graceCtx.
	graceCtx, graceCancel := WithGracePeriod(ctx, e.SignalGracePeriod)
	defer graceCancel()
	go func() {
		<-e.cancelCh
		e.shell.Commentf("Received cancellation signal, interrupting")
		cancel()
	}()

	// Create an empty env for us to keep track of our env changes in
	e.shell.Env = env.FromSlice(os.Environ())

	// Initialize the job API, iff the experiment is enabled. Noop otherwise
	if e.JobAPI {
		cleanup, err := e.startJobAPI()
		if err != nil {
			e.shell.Errorf("Error setting up Job API: %v", err)
			return 1
		}
		defer cleanup()
	} else {
		e.shell.OptionalWarningf("job-api-disabled", "The Job API has been disabled. Features like automatic redaction of secrets and polyglot hooks will either not work or have degraded functionality")
	}

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		// We strive to let the executor tear-down happen whether or not the job
		// (and thus ctx) is cancelled, so it can run during the grace period.
		if err := e.tearDown(graceCtx); err != nil {
			e.shell.Errorf("Error tearing down job executor: %v", err)

			// this gets passed back via the named return
			exitCode = shell.ExitCode(err)
		}
	}()

	if env, ok := e.shell.Env.Get("BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS"); ok && env == "true" {
		// On hosted compute, we are not going to use SSH keys, so we don't need to scan for SSH keys.
		//
		// TODO: This may break non-GitHub SSH checkout for other SCMs on self-hosted compute.
		// So we need to revise this before enabling the code access app on self-hosted agents.
		e.SSHKeyscan = false

		err := e.configureGitCredentialHelper(ctx)
		if err != nil {
			e.shell.Errorf("Error configuring git credential helper: %v", err)
			return shell.ExitCode(err)
		}

		// so that the new credential helper will be used for all github urls
		err = e.configureHTTPSInsteadOfSSH(ctx)
		if err != nil {
			e.shell.Errorf("Error configuring https instead of ssh: %v", err)
			return shell.ExitCode(err)
		}
	}

	// Initialize the environment, a failure here will still call the tearDown
	if err = e.setUp(ctx); err != nil {
		e.shell.Errorf("Error setting up job executor: %v", err)
		return shell.ExitCode(err)
	}

	// Execute the job phases in order
	var phaseErr error

	if e.includePhase("plugin") {
		phaseErr = e.preparePlugins()

		if phaseErr == nil {
			phaseErr = e.PluginPhase(ctx)
		}
	}

	if phaseErr == nil && e.includePhase("checkout") {
		phaseErr = e.CheckoutPhase(ctx)
	} else {
		// For various reasons we should still pretend there was a checkout
		// phase. It might have happened in a different container, or may have
		// been disabled, but there can be important files at the checkout path,
		// e.g. local hooks, which require a checkout root.
		checkoutDir, exists := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
		if exists {
			_ = e.shell.Chdir(checkoutDir)
		}
		root, err := os.OpenRoot(e.shell.Getwd())
		if err != nil {
			phaseErr = cmp.Or(phaseErr, err)
		}
		if root != nil {
			e.checkoutRoot = root
			runtime.AddCleanup(e, func(r *os.Root) { r.Close() }, root)
		}
	}

	if phaseErr == nil && e.includePhase("plugin") {
		phaseErr = e.VendoredPluginPhase(ctx)
	}

	if phaseErr == nil && e.includePhase("command") {
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

		// Only upload artifacts as part of the command phase.
		// The artifacts might be relevant for debugging job timeouts, so it can
		// run during the grace period.
		if err := e.artifactPhase(graceCtx); err != nil {
			e.shell.Errorf("%v", err)

			if commandErr != nil {
				// Both command and upload have errored.
				//
				// Ignore the agent upload error, rely on the phase and command
				// error reporting below.
			} else {
				// Only upload has errored, report its error.
				return shell.ExitCode(err)
			}
		}
	}

	// Phase errors are where something of ours broke that merits a big red error
	// this won't include command failures, as we view that as more in the user space
	if phaseErr != nil {
		err = phaseErr
		e.shell.Errorf("%v", phaseErr)
		return shell.ExitCode(phaseErr)
	}

	// Use the exit code from the command phase
	exitStatus, _ := e.shell.Env.Get("BUILDKITE_COMMAND_EXIT_STATUS")
	exitStatusCode, _ := strconv.Atoi(exitStatus)

	return exitStatusCode
}

func (e *Executor) includePhase(phase string) bool {
	if len(e.Phases) == 0 {
		return true
	}
	return slices.Contains(e.Phases, phase)
}

// Cancel interrupts any running shell processes and causes the job to stop.
func (e *Executor) Cancel() error {
	// Closing e.cancelCh broadcasts to any goroutine receiving that the job is
	// being cancelled/stopped.
	// Double-closing a channel is a panic, so guard it with a bool and a mutex.
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()
	if e.cancelled {
		return errors.New("already cancelled")
	}
	e.cancelled = true
	e.shell.Env.Set("BUILDKITE_JOB_CANCELLED", "true")
	close(e.cancelCh)
	return nil
}

const (
	HookScopeAgent      = "agent"
	HookScopeRepository = "repository"
	HookScopePlugin     = "plugin"
)

type HookConfig struct {
	Name           string
	Scope          string
	Path           string
	Env            *env.Environment
	SpanAttributes map[string]string
	PluginName     string
}

func (e *Executor) tracingImplementationSpecificHookScope(scope string) string {
	if e.TracingBackend != tracetools.BackendDatadog {
		return scope
	}

	// In olden times, when the datadog tracing backend was written, these hook scopes were named "local" and "global"
	// We need to maintain backwards compatibility with the old names for span attribute reasons, so we map them here
	switch scope {
	case HookScopeRepository:
		return "local"
	case HookScopeAgent:
		return "global"
	default:
		return scope
	}
}

// executeHook runs a hook script with the hookRunner
func (e *Executor) executeHook(ctx context.Context, hookCfg HookConfig) error {
	scopeName := e.tracingImplementationSpecificHookScope(hookCfg.Scope)
	spanName := e.implementationSpecificSpanName(fmt.Sprintf("%s %s hook", scopeName, hookCfg.Name), "hook.execute")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.TracingBackend)
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

	if !osutil.FileExists(hookCfg.Path) {
		if e.Debug {
			e.shell.Commentf("Skipping %s hook, no script at \"%s\"", hookName, hookCfg.Path)
		}
		return nil
	}

	e.shell.Headerf("Running %s hook", hookName)

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

func (e *Executor) runUnwrappedHook(ctx context.Context, _ string, hookCfg HookConfig) error {
	environ := hookCfg.Env.Copy()

	environ.Set("BUILDKITE_HOOK_PHASE", hookCfg.Name)
	environ.Set("BUILDKITE_HOOK_PATH", hookCfg.Path)
	environ.Set("BUILDKITE_HOOK_SCOPE", hookCfg.Scope)

	if err := e.shell.Command(hookCfg.Path).Run(ctx, shell.WithExtraEnv(environ)); err != nil {
		return err
	}
	// Passing an empty env changes through because in polyglot hook we can't detect
	// env change.
	// But we call this method anyway because a hook might use buildkite-agent env set to update environment.
	e.applyEnvironmentChanges(hook.EnvChanges{})
	return nil
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
	case osutil.FileExists("/dev/fd"):
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

func logMissingHookInfo(l shell.Logger, hookName, wrapperPath string) {
	// It's unlikely, but possible, that the script wrapper was spontaneously
	// deleted or corrupted (it's usually in /tmp, which is fair game).
	// A common setup error is to try to run a Bash hook in a container or other
	// environment without Bash (or Bash is not in the expected location).
	shebang, err := shellscript.ShebangLine(wrapperPath)
	if err != nil {
		// It's reasonable to assume the script wrapper was spontaneously
		// deleted, or had something equally horrible happen to it.
		l.Errorf("The %s hook failed to run - perhaps the wrapper script %q was spontaneously deleted", hookName, wrapperPath)
		return
	}
	interpreter := strings.TrimPrefix(shebang, "#!")
	if interpreter == "" {
		// Either the script never had a shebang, or the script was
		// spontaneously corrupted.
		// If it didn't have a shebang line, we defaulted to using Bash, and if
		// that's not present we already logged a warning.
		// If it was spontaneously corrupted, we should expect a different error
		// than ENOENT.
		return
	}
	l.Errorf("The %s hook failed to run - perhaps the script interpreter %q is missing", hookName, interpreter)
}

func (e *Executor) runWrappedShellScriptHook(ctx context.Context, hookName string, hookCfg HookConfig) error {
	defer e.redactors.Flush()

	script, err := hook.NewWrapper(hook.WithPath(hookCfg.Path))
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

	const maxHookRetry = 30

	// Run the wrapper script
	err = roko.NewRetrier(
		roko.WithStrategy(roko.Constant(100*time.Millisecond)),
		roko.WithMaxAttempts(maxHookRetry),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Run the script and only retry on ETXTBSY.
		// This error occurs because of an unavoidable race between forking
		// (which acquires open file descriptors of the parent process) and
		// writing an executable (the script wrapper).
		// See https://github.com/golang/go/issues/22315.
		script, err := e.shell.Script(script.Path())
		if err != nil {
			r.Break()
			return err
		}
		err = script.Run(ctx, shell.ShowPrompt(false), shell.WithExtraEnv(hookCfg.Env))
		if errors.Is(err, syscall.ETXTBSY) {
			return err
		}
		r.Break()
		return err
	})
	if err != nil {
		exitCode := shell.ExitCode(err)
		e.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", strconv.Itoa(exitCode))

		// If the hook exited with a non-zero exit code, then we should pass that back to the executor
		// so it may inform the Buildkite API
		if shell.IsExitError(err) {
			return &shell.ExitError{
				Code: exitCode,
				Err:  fmt.Errorf("The %s hook exited with status %d", hookName, exitCode),
			}
		}

		switch {
		case errors.Is(err, syscall.ETXTBSY):
			// If the underlying error is _still_ ETXTBSY, then inspect the file
			// to see what process had it open for write, to log something helpful
			logOpenedHookInfo(e.shell.Logger, e.Debug, hookName, script.Path())

		case errors.Is(err, syscall.ENOENT):
			// Unfortunately the wrapping os.PathError's Path is always the
			// program we tried to exec, even if the missing file/directory was
			// actually the interpreter specified on the shebang line.
			// Try to figure out which part is missing from the wrapper.
			logMissingHookInfo(e.shell.Logger, hookName, script.Path())
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
		e.applyEnvironmentChanges(changes)
	}

	return nil
}

// 1. Apply env changes -> e.shell.Env
// 2. Refresh executor config (e.shell.Env might change via Job API)
// 3. Log all changes.
func (e *Executor) applyEnvironmentChanges(changes hook.EnvChanges) {
	if afterWd, err := changes.GetAfterWd(); err == nil {
		if afterWd != e.shell.Getwd() {
			_ = e.shell.Chdir(afterWd)
		}
	}

	e.shell.Env.Apply(changes.Diff)
	e.addOutputRedactors()

	// First, let see any of the environment variables are supposed
	// to change the job configuration at run time.
	// Note this func mutates/refreshes the ExecutorConfig too.
	executorConfigEnvChanges := e.ReadFromEnvironment(e.shell.Env)

	// Print out the env vars that changed. As we go through each
	// one, we'll determine if it was a special environment variable
	// that has changed the executor configuration at runtime.
	//
	// If it's "special", we'll show the value it was changed to -
	// otherwise we'll hide it. Since we don't know if an
	// environment variable contains sensitive information (such as
	// THIRD_PARTY_API_KEY) we'll just not show any values for
	// anything not controlled by us.
	executorConfigEnvChangesLogged := make(map[string]bool)

	for k, v := range changes.Diff.Added {
		if _, ok := executorConfigEnvChanges[k]; ok {
			executorConfigEnvChangesLogged[k] = true
			e.shell.Commentf("%s is now %q", k, v)
		} else {
			e.shell.Commentf("%s added", k)
		}
	}
	for k, v := range changes.Diff.Changed {
		if _, ok := executorConfigEnvChanges[k]; ok {
			executorConfigEnvChangesLogged[k] = true
			e.shell.Commentf("%s was %q and is now %q", k, v.Old, v.New)
		} else {
			e.shell.Commentf("%s changed", k)
		}
	}
	for k, v := range changes.Diff.Removed {
		if _, ok := executorConfigEnvChanges[k]; ok {
			executorConfigEnvChangesLogged[k] = true
			e.shell.Commentf("%s is now %q", k, v)
		} else {
			e.shell.Commentf("%s removed", k)
		}
	}

	// When an env var is changed via buildkite-agent env set instead,
	// it might not appear in the script "changes".
	for k, v := range executorConfigEnvChanges {
		if !executorConfigEnvChangesLogged[k] {
			e.shell.Commentf("%s is now %q", k, v)
		}
	}
}

// Should be called whenever we updated our e.shell.Env.
func (e *Executor) addOutputRedactors() {
	// reset output redactors based on new environment variable values
	toRedact, short, err := redact.Vars(e.RedactedVars, e.shell.Env.DumpPairs())
	if err != nil {
		e.shell.OptionalWarningf("bad-redacted-vars", "Couldn't match environment variable names against redacted-vars: %v", err)
	}
	if len(short) > 0 {
		slices.Sort(short)
		e.shell.OptionalWarningf("short-redacted-vars", "Some variables have values below minimum length (%d bytes) and will not be redacted: %s", redact.LengthMin, strings.Join(short, ", "))
	}

	// This should probably be a reset rather than a mutate.
	// But does a particular string stop being a secret if we learn new secret strings?
	// For produence, we use Add.
	for _, pair := range toRedact {
		e.redactors.Add(pair.Value)
	}
}

func (e *Executor) hasGlobalHook(name string) bool {
	_, err := hook.Find(nil, e.HooksPath, name)
	if err == nil {
		return true
	}
	for _, additional := range e.AdditionalHooksPaths {
		_, err := hook.Find(nil, additional, name)
		if err == nil {
			return true
		}
	}
	return false
}

// find all matching paths for the specified hook
func (e *Executor) getAllGlobalHookPaths(name string) ([]string, error) {
	hooks := []string{}
	p, err := hook.Find(nil, e.HooksPath, name)
	if err != nil {
		if !os.IsNotExist(err) {
			return []string{}, err
		}
	} else {
		hooks = append(hooks, p)
	}

	for _, additional := range e.AdditionalHooksPaths {
		p, err = hook.Find(nil, additional, name)
		// as this is an additional hook, don't fail if there's a problem here
		if err == nil {
			hooks = append(hooks, p)
		}
	}

	return hooks, nil
}

// Executes a global hook if one exists
func (e *Executor) executeGlobalHook(ctx context.Context, name string) error {
	allHooks, err := e.getAllGlobalHookPaths(name)
	if err != nil {
		return nil
	}
	for _, h := range allHooks {
		err = e.executeHook(ctx, HookConfig{
			Scope: HookScopeAgent,
			Name:  name,
			Path:  h,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Returns the absolute path to a local hook, or os.ErrNotExist if none is found
func (e *Executor) localHookPath(name string) (string, error) {
	// The local hooks dir must exist within the checkout root.
	dir := filepath.Join(".buildkite", "hooks")
	return hook.Find(e.checkoutRoot, dir, name)
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
			if e.Debug {
				e.shell.Commentf("Local hook %s doesn't exist: %s, skipping", name, err)
			}
			return nil
		}

		// This should not be possible under the current state of the code base
		// as hook.Find only returns os.ErrNotExist but that assumes implementation
		// details that could change in the future
		return err
	}

	// For high-security configs, we allow the disabling of local hooks.
	localHooksEnabled := e.LocalHooksEnabled

	// Allow hooks to disable local hooks by setting BUILDKITE_NO_LOCAL_HOOKS=true
	noLocalHooks, _ := e.shell.Env.Get("BUILDKITE_NO_LOCAL_HOOKS")
	if noLocalHooks == "true" || noLocalHooks == "1" {
		localHooksEnabled = false
	}

	if !localHooksEnabled {
		return fmt.Errorf("Refusing to run %s, local hooks are disabled", localHookPath)
	}

	return e.executeHook(ctx, HookConfig{
		Scope: HookScopeRepository,
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
	if osutil.FileExists(repository) {
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
	span, ctx := tracetools.StartSpanFromContext(ctx, "environment", e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if e.BinPath != "" {
		path, _ := e.shell.Env.Get("PATH")
		// BinPath goes last so we don't disturb other tools
		e.shell.Env.Set("PATH", fmt.Sprintf("%s%c%s", path, os.PathListSeparator, e.BinPath))
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
	if ignored, _ := e.shell.Env.Get("BUILDKITE_IGNORED_ENV"); ignored != "" {
		e.shell.Headerf("Detected protected environment variables")
		e.shell.Commentf("Your pipeline environment has protected environment variables set. " +
			"These can only be set via hooks, plugins or the agent configuration.")

		for env := range strings.SplitSeq(ignored, ",") {
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
				e.shell.Printf("%s", strings.ReplaceAll(envar, "\n", "\\n"))
			}
		}
	}

	// Disable any interactive Git/SSH prompting
	e.shell.Env.Set("GIT_TERMINAL_PROMPT", "0")

	// Fetch and set secrets before environment hook execution
	if e.Secrets != "" {
		if err := e.fetchAndSetSecrets(ctx); err != nil {
			return fmt.Errorf("failed to fetch secrets for job: %w", err)
		}
	}

	// It's important to do this before checking out plugins, in case you want
	// to use the global environment hook to whitelist the plugins that are
	// allowed to be used.
	err = e.executeGlobalHook(ctx, "environment")
	return err
}

// fetchAndSetSecrets handles secrets fetching and processing directly
func (e *Executor) fetchAndSetSecrets(ctx context.Context) error {
	if e.Secrets == "" {
		return nil // No secrets to process
	}

	// Parse secrets from JSON using the pipeline.Secret type
	var pipelineSecrets []*pipeline.Secret
	if err := json.Unmarshal([]byte(e.Secrets), &pipelineSecrets); err != nil {
		return fmt.Errorf("failed to parse secrets JSON: %w", err)
	}

	if len(pipelineSecrets) == 0 {
		return nil // No secrets to process
	}

	e.shell.Headerf("Preparing secrets")

	// Extract keys for fetching
	keys := make([]string, len(pipelineSecrets))
	for i, ps := range pipelineSecrets {
		keys[i] = ps.Key
	}

	// Create API client for fetching secrets.
	secretLogger := logger.NewBuffer()
	apiClient := api.NewClient(secretLogger, api.Config{
		Endpoint: e.shell.Env.GetString("BUILDKITE_AGENT_ENDPOINT", ""),
		Token:    e.shell.Env.GetString("BUILDKITE_AGENT_ACCESS_TOKEN", ""),
	})

	// Fetch all secrets. We pass secretLogger (a buffer) here because
	// FetchSecrets takes a logger.Logger, but retry warnings within it need
	// to reach the user. We flush those from the buffer afterwards.
	fetchedSecrets, errs := secrets.FetchSecrets(ctx, secretLogger, apiClient, e.JobID, keys, 10)

	// Surface any retry warnings that were buffered during fetching.
	// The buffer logger prefixes warn messages with "[warn] ".
	for _, msg := range secretLogger.Messages {
		if after, ok := strings.CutPrefix(msg, "[warn] "); ok {
			e.shell.Warningf("%s", after)
		}
	}

	if len(errs) > 0 {
		var errorMsg strings.Builder
		for _, err := range errs {
			fmt.Fprintf(&errorMsg, "\n   %s", err)
		}
		return errors.New(errorMsg.String())
	}

	secretValuesByKey := make(map[string]string, len(fetchedSecrets))
	for _, fetchedSecret := range fetchedSecrets {
		secretValuesByKey[fetchedSecret.Key] = fetchedSecret.Value
	}

	// Set environment variables and register for redaction
	for _, pipelineSecret := range pipelineSecrets {
		if secretValue, exists := secretValuesByKey[pipelineSecret.Key]; exists {
			// Always register the secret value for redaction regardless of env var setting
			e.redactors.Add(secretValue)

			// Set the environment variable only if environment_variable is specified and non-nil
			if pipelineSecret.EnvironmentVariable != "" {
				// Check if the environment variable is protected
				if env.IsProtected(pipelineSecret.EnvironmentVariable) {
					return fmt.Errorf("secret %q cannot set protected environment variable %q", pipelineSecret.Key, pipelineSecret.EnvironmentVariable)
				}

				var alreadySet bool
				_, alreadySet = e.shell.Env.Get(pipelineSecret.EnvironmentVariable)

				e.shell.Env.Set(pipelineSecret.EnvironmentVariable, secretValue)

				if alreadySet {
					e.shell.Commentf("Secret %s added as environment variable %s (overwritten)", pipelineSecret.Key, pipelineSecret.EnvironmentVariable)
				} else {
					e.shell.Commentf("Secret %s added as environment variable %s", pipelineSecret.Key, pipelineSecret.EnvironmentVariable)
				}
			}
		}
	}

	return nil
}

// tearDown is called before the executor exits, even on error
func (e *Executor) tearDown(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-exit", e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	// In vanilla agent usage, there's always a command phase.
	// But over in agent-stack-k8s, which splits the agent phases among
	// containers (the checkout phase happens in a separate container to the
	// command phase), the two phases have different environments.
	// Unfortunately pre-exit hooks are often not written with this split in
	// mind.
	if e.includePhase("command") {
		if err = e.executeGlobalHook(ctx, "pre-exit"); err != nil {
			return err
		}

		if err = e.executeLocalHook(ctx, "pre-exit"); err != nil {
			return err
		}

		if err = e.executePluginHook(ctx, "pre-exit", e.pluginCheckouts); err != nil {
			return err
		}
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
func (e *Executor) runPreCommandHooks(ctx context.Context) (err error) {
	spanName := e.implementationSpecificSpanName("pre-command", "pre-command hooks")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.TracingBackend)
	defer func() { span.FinishWithError(err) }()

	if err := e.executeGlobalHook(ctx, "pre-command"); err != nil {
		return err
	}
	if err := e.executeLocalHook(ctx, "pre-command"); err != nil {
		return err
	}
	return e.executePluginHook(ctx, "pre-command", e.pluginCheckouts)
}

// runCommand runs the command and adds tracing spans.
func (e *Executor) runCommand(ctx context.Context) error {
	// There can only be one command hook, so we check them in order of plugin, local
	switch {
	case e.hasPluginHook("command"):
		return e.executePluginHook(ctx, "command", e.pluginCheckouts)
	case e.hasLocalHook("command"):
		return e.executeLocalHook(ctx, "command")
	case e.hasGlobalHook("command"):
		return e.executeGlobalHook(ctx, "command")
	default:
		return e.defaultCommandPhase(ctx)
	}
}

// runPostCommandHooks runs the post-command hooks and adds tracing spans.
func (e *Executor) runPostCommandHooks(ctx context.Context) (err error) {
	spanName := e.implementationSpecificSpanName("post-command", "post-command hooks")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.TracingBackend)
	defer func() { span.FinishWithError(err) }()

	if err := e.executeGlobalHook(ctx, "post-command"); err != nil {
		return err
	}
	if err := e.executeLocalHook(ctx, "post-command"); err != nil {
		return err
	}
	return e.executePluginHook(ctx, "post-command", e.pluginCheckouts)
}

// CommandPhase determines how to run the build, and then runs it
func (e *Executor) CommandPhase(ctx context.Context) (hookErr, commandErr error) {
	var preCommandErr error

	span, ctx := tracetools.StartSpanFromContext(ctx, "command", e.TracingBackend)
	defer func() {
		span.FinishWithError(hookErr)
	}()

	// Run postCommandHooks, even if there is an error from the command, but not if there is an
	// error from the pre-command hooks. Note: any post-command hook error will be returned.
	defer func() {
		if preCommandErr != nil {
			return
		}
		// Because post-command hooks are often used for post-job cleanup, they
		// can run during the grace period.
		graceCtx, cancel := WithGracePeriod(ctx, e.SignalGracePeriod)
		defer cancel()
		hookErr = e.runPostCommandHooks(graceCtx)
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
		strconv.Itoa(shell.ExitCode(commandErr)),
	)

	// Exit early if there was no error
	if commandErr == nil {
		return nil, nil
	}

	// Expand the job log header from the command to surface the error
	e.shell.Printf("^^^ +++")

	isExitError := shell.IsExitError(commandErr)
	isExitSignaled := shell.IsExitSignaled(commandErr)

	switch {
	case isExitError && isExitSignaled:
		e.shell.Errorf("The command was interrupted by a signal: %v", commandErr)
		return nil, commandErr

	case isExitError && !isExitSignaled:
		e.shell.Errorf("The command exited with status %d", shell.ExitCode(commandErr))
		return nil, commandErr

	default:
		e.shell.Errorf("%s", commandErr)

		// error is not an exit error, we don't want to return it
		return nil, nil
	}
}

// defaultCommandPhase is executed if there is no global or plugin command hook
func (e *Executor) defaultCommandPhase(ctx context.Context) error {
	defer e.redactors.Flush()

	spanName := e.implementationSpecificSpanName("default command hook", "hook.execute")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.TracingBackend)
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

	scriptFileName := strings.ReplaceAll(e.Command, "\n", "")
	pathToCommand, err := filepath.Abs(filepath.Join(e.shell.Getwd(), scriptFileName))
	commandIsScript := err == nil && osutil.FileExists(pathToCommand)
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

	// The interpreter gets parsed based on the operating system
	interpreter, err := shellwords.Split(e.Shell)
	if err != nil {
		return fmt.Errorf("Failed to split shell (%q) into tokens: %w", e.Shell, err)
	}

	if len(interpreter) == 0 {
		return fmt.Errorf("No shell set for job")
	}

	// Windows CMD.EXE is horrible and can't handle newline delimited commands. We write
	// a batch script so that it works, but we don't like it
	if strings.ToUpper(filepath.Base(interpreter[0])) == "CMD.EXE" {
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
		if e.CommandEval {
			// Make script executable
			if err = osutil.ChmodExecutable(pathToCommand); err != nil {
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

	var cmd []string
	cmd = append(cmd, interpreter...)
	cmd = append(cmd, cmdToExec)

	if e.Debug {
		e.shell.Promptf("%s", process.FormatCommand(cmd[0], cmd[1:]))
	} else {
		e.shell.Promptf("%s", cmdToExec)
	}

	err = e.shell.Command(cmd[0], cmd[1:]...).Run(ctx, shell.ShowPrompt(false))
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

	scriptContents := []string{"@echo off"}

	for line := range strings.SplitSeq(cmd, "\n") {
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

// setupRedactors creates new stdout and [shell.Logger] to use for a new shell,
// that write to stdout and stderr respectively, each via a [replacer.Replacer]
// set up as a secret redactor. References to the redactors are retained in
// e.redactors so they can be updated with new secrets.
//
// Pictorally:
//
//	(returned io.Writer) == redactor 1 -> stdout
//
// and
//
//	(returned shell.Logger) -> redactor 2 -> stderr
func (e *Executor) setupRedactors(log shell.Logger, environ *env.Environment, stdout, stderr io.Writer) (io.Writer, shell.Logger) {
	varsToRedact, short, err := redact.Vars(e.RedactedVars, environ.DumpPairs())
	if err != nil {
		log.OptionalWarningf("bad-redacted-vars", "Couldn't match environment variable names against redacted-vars: %v", err)
	}
	if len(short) > 0 {
		slices.Sort(short)
		log.OptionalWarningf("short-redacted-vars", "Some variables have values below minimum length (%d bytes) and will not be redacted: %s", redact.LengthMin, strings.Join(short, ", "))
	}

	if e.Debug {
		log.Commentf("Enabling output redaction for values from environment variables matching: %v", e.RedactedVars)
	}

	needles := make([]string, 0, len(varsToRedact))
	for _, pair := range varsToRedact {
		needles = append(needles, pair.Value)
	}

	stdoutRedactor := replacer.New(stdout, needles, redact.Redacted)
	e.redactors.Append(stdoutRedactor)
	loggerRedactor := replacer.New(stderr, needles, redact.Redacted)
	e.redactors.Append(loggerRedactor)

	logger := shell.NewWriterLogger(loggerRedactor, true, e.DisabledWarnings)
	return stdoutRedactor, logger
}
