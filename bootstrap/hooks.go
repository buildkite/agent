package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/redaction"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/utils"
	"github.com/pkg/errors"
)

type HookConfig struct {
	Name           string
	Scope          string
	Path           string
	Env            env.Environment
	SpanAttributes map[string]string
}

func (b *Bootstrap) tracingImplementationSpecificHookScope(scope string) string {
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
func (b *Bootstrap) executeHook(ctx context.Context, hookCfg HookConfig) error {
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

	hookName := hookCfg.Scope + " " + hookCfg.Name

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
	script, err := hook.CreateScriptWrapper(hookCfg.Path)
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
			return errors.Wrapf(err, "Failed to get environment")
		}
	} else {
		// Hook exited successfully (and not early!) We have an environment and
		// wd change we can apply to our subsequent phases
		b.applyEnvironmentChanges(changes, redactors)
	}

	return nil
}

func (b *Bootstrap) applyEnvironmentChanges(changes hook.HookScriptChanges, redactors redaction.RedactorMux) {
	if afterWd, err := changes.GetAfterWd(); err == nil {
		if afterWd != b.shell.Getwd() {
			_ = b.shell.Chdir(afterWd)
		}
	}

	// Do we even have any environment variables to change?
	if changes.Diff.Empty() {
		return
	}

	mergedEnv := b.shell.Env.Apply(changes.Diff)

	// reset output redactors based on new environment variable values
	redactors.Flush()
	redactors.Reset(redaction.GetValuesToRedact(b.shell, b.Config.RedactedVars, mergedEnv))

	// First, let see any of the environment variables are supposed
	// to change the bootstrap configuration at run time.
	bootstrapConfigEnvChanges := b.Config.ReadFromEnvironment(mergedEnv)

	// Print out the env vars that changed. As we go through each
	// one, we'll determine if it was a special "bootstrap"
	// environment variable that has changed the bootstrap
	// configuration at runtime.
	//
	// If it's "special", we'll show the value it was changed to -
	// otherwise we'll hide it. Since we don't know if an
	// environment variable contains sensitive information (such as
	// THIRD_PARTY_API_KEY) we'll just not show any values for
	// anything not controlled by us.
	for k, v := range changes.Diff.Added {
		if _, ok := bootstrapConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s added", k)
		}
	}
	for k, v := range changes.Diff.Changed {
		if _, ok := bootstrapConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s changed", k)
		}
	}
	for k, v := range changes.Diff.Removed {
		if _, ok := bootstrapConfigEnvChanges[k]; ok {
			b.shell.Commentf("%s is now %q", k, v)
		} else {
			b.shell.Commentf("%s removed", k)
		}
	}

	// Now that we've finished telling the user what's changed,
	// let's mutate the current shell environment to include all
	// the new values.
	b.shell.Env = mergedEnv
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
	return b.executeHook(ctx, HookConfig{
		Scope: "global",
		Name:  name,
		Path:  p,
	})
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

	return b.executeHook(ctx, HookConfig{
		Scope: "local",
		Name:  name,
		Path:  localHookPath,
	})
}
