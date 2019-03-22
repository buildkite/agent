package agent

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/logger"
)

// AgentConfiguration is the run-time configuration for an agent that
// has been loaded from the config file and command-line params
type AgentConfiguration struct {
	ConfigPath                 string
	BootstrapScript            string
	BuildPath                  string
	HooksPath                  string
	GitMirrorsPath             string
	GitMirrorsLockTimeout      int
	PluginsPath                string
	GitCloneFlags              string
	GitCloneMirrorFlags        string
	GitCleanFlags              string
	GitSubmodules              bool
	SSHKeyscan                 bool
	CommandEval                bool
	PluginsEnabled             bool
	PluginValidation           bool
	LocalHooksEnabled          bool
	RunInPty                   bool
	DisableColors              bool
	TimestampLines             bool
	DisconnectAfterJob         bool
	DisconnectAfterJobTimeout  int
	DisconnectAfterIdleTimeout int
	CancelGracePeriod          int
	Shell                      string
}

// ShowBanner prints a welcome banner and the configuration options
func ShowBanner(l *logger.Logger, conf AgentConfiguration) {
	welcomeMessage :=
		"\n" +
			"%s  _           _ _     _ _    _ _                                _\n" +
			" | |         (_) |   | | |  (_) |                              | |\n" +
			" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
			" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
			" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
			" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
			"                                                __/ |\n" +
			" http://buildkite.com/agent                    |___/\n%s\n"

	if !conf.DisableColors {
		fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
	} else {
		fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
	}

	l.Notice("Starting buildkite-agent v%s with PID: %s", Version(), fmt.Sprintf("%d", os.Getpid()))
	l.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	l.Notice("For questions and support, email us at: hello@buildkite.com")

	if conf.ConfigPath != "" {
		l.Info("Configuration loaded from: %s", conf.ConfigPath)
	}

	l.Debug("Bootstrap command: %s", conf.BootstrapScript)
	l.Debug("Build path: %s", conf.BuildPath)
	l.Debug("Hooks directory: %s", conf.HooksPath)
	l.Debug("Plugins directory: %s", conf.PluginsPath)

	if !conf.SSHKeyscan {
		l.Info("Automatic ssh-keyscan has been disabled")
	}

	if !conf.CommandEval {
		l.Info("Evaluating console commands has been disabled")
	}

	if !conf.PluginsEnabled {
		l.Info("Plugins have been disabled")
	}

	if !conf.RunInPty {
		l.Info("Running builds within a pseudoterminal (PTY) has been disabled")
	}

	if conf.DisconnectAfterJob {
		l.Info("Agent will disconnect after a job run has completed with a timeout of %d seconds",
			conf.DisconnectAfterJobTimeout)
	}
}
