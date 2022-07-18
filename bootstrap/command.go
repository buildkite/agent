package bootstrap

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/utils"
	"github.com/buildkite/shellwords"
)

// runPreCommandHooks runs the pre-command hooks and adds tracing spans.
func (b *Bootstrap) runPreCommandHooks(ctx context.Context) error {
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
func (b *Bootstrap) runCommand(ctx context.Context) error {
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
func (b *Bootstrap) runPostCommandHooks(ctx context.Context) error {
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
func (b *Bootstrap) CommandPhase(ctx context.Context) (error, error) {
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
func (b *Bootstrap) defaultCommandPhase(ctx context.Context) error {
	spanName := b.implementationSpecificSpanName("hook.default.command", "hook.execute")
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

func (b *Bootstrap) writeBatchScript(cmd string) (string, error) {
	scriptFile, err := shell.TempFileWithExtension(
		`buildkite-script.bat`,
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
