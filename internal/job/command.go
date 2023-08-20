package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/shellscript"
	"github.com/buildkite/agent/v3/internal/utils"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/shellwords"
)

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
