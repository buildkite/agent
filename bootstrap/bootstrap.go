package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/redaction"
	"github.com/buildkite/agent/v3/tracetools"
	"golang.org/x/exp/slices"
)

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

func (b *Bootstrap) hasPhase(phase string) bool {
	if len(b.Phases) == 0 {
		return true
	}
	return slices.Contains(b.Phases, phase)
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
		b.shell.InterruptSignal = b.Config.CancelSignal
	}

	var err error

	span, ctx, stopper := b.startTracing(ctx)
	defer stopper()
	defer func() { span.FinishWithError(err) }()

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

	//  Execute the bootstrap phases in order
	var phaseErr error

	if b.hasPhase("plugin") {
		phaseErr = b.preparePlugins()

		if phaseErr == nil {
			phaseErr = b.PluginPhase(ctx)
		}
	}

	if phaseErr == nil && b.hasPhase("checkout") {
		phaseErr = b.CheckoutPhase(ctx)
	} else {
		checkoutDir, exists := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
		if exists {
			_ = b.shell.Chdir(checkoutDir)
		}
	}

	if phaseErr == nil && b.hasPhase("plugin") {
		phaseErr = b.VendoredPluginPhase(ctx)
	}

	if phaseErr == nil && b.hasPhase("command") {
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

// Cancel interrupts any running shell processes and causes the bootstrap to stop
func (b *Bootstrap) Cancel() error {
	b.cancelCh <- struct{}{}
	return nil
}

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

// setUp is run before all the phases run. It's responsible for initializing the
// bootstrap environment
func (b *Bootstrap) setUp(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "environment", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	// Create an empty env for us to keep track of our env changes in
	b.shell.Env = env.FromSlice(os.Environ())

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

// tearDown is called before the bootstrap exits, even on error
func (b *Bootstrap) tearDown(ctx context.Context) error {
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
		return tearDownDeprecatedDockerIntegration(b.shell)
	}

	for _, dir := range b.cleanupDirs {
		if err = os.RemoveAll(dir); err != nil {
			b.shell.Warningf("Failed to remove dir %s: %v", dir, err)
		}
	}

	return nil
}

// setupRedactors wraps shell output and logging in Redactor if any redaction
// is necessary based on RedactedVars configuration and the existence of
// matching environment vars.
// redaction.RedactorMux (possibly empty) is returned so the caller can `defer redactor.Flush()`
func (b *Bootstrap) setupRedactors() redaction.RedactorMux {
	valuesToRedact := redaction.GetValuesToRedact(b.shell, b.Config.RedactedVars, b.shell.Env)
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
