package clicommand

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/template"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/job"
	"github.com/buildkite/agent/v3/process"
	"github.com/urfave/cli"
)

var jobRunHelpTpl = template.Must(template.New("jobRunHelp").Parse(`Usage:

   buildkite-agent {{.}} [options...]

Description:

   The {{.}} command executes a Buildkite job locally.

   Generally the {{.}} command is run as a sub-process of the buildkite-agent to execute
   a given job sent from buildkite.com, but you can also invoke the {{.}} manually.

   Execution is broken down into phases. By default, the {{.}} runs a plugin phase which
   sets up any plugins specified, then a checkout phase which pulls down your code and then a
   command phase that executes the specified command in the created environment.

   You can run only specific phases with the --phases flag.

   The {{.}} is also responsible for executing hooks around the phases.
   See https://buildkite.com/docs/agent/v3/hooks for more details.

Example:

   $ eval $(curl -s -H "Authorization: Bearer xxx" \
     "https://api.buildkite.com/v2/organizations/[org]/pipelines/[proj]/builds/[build]/jobs/[job]/env.txt" | sed 's/^/export /')
   $ buildkite-agent {{.}} --build-path builds`))

type JobRunConfig struct {
	Command                      string   `cli:"command"`
	JobID                        string   `cli:"job" validate:"required"`
	Repository                   string   `cli:"repository" validate:"required"`
	Commit                       string   `cli:"commit" validate:"required"`
	Branch                       string   `cli:"branch" validate:"required"`
	Tag                          string   `cli:"tag"`
	RefSpec                      string   `cli:"refspec"`
	Plugins                      string   `cli:"plugins"`
	PullRequest                  string   `cli:"pullrequest"`
	GitSubmodules                bool     `cli:"git-submodules"`
	SSHKeyscan                   bool     `cli:"ssh-keyscan"`
	AgentName                    string   `cli:"agent" validate:"required"`
	Queue                        string   `cli:"queue"`
	OrganizationSlug             string   `cli:"organization" validate:"required"`
	PipelineSlug                 string   `cli:"pipeline" validate:"required"`
	PipelineProvider             string   `cli:"pipeline-provider" validate:"required"`
	AutomaticArtifactUploadPaths string   `cli:"artifact-upload-paths"`
	ArtifactUploadDestination    string   `cli:"artifact-upload-destination"`
	CleanCheckout                bool     `cli:"clean-checkout"`
	GitCheckoutFlags             string   `cli:"git-checkout-flags"`
	GitCloneFlags                string   `cli:"git-clone-flags"`
	GitFetchFlags                string   `cli:"git-fetch-flags"`
	GitCloneMirrorFlags          string   `cli:"git-clone-mirror-flags"`
	GitCleanFlags                string   `cli:"git-clean-flags"`
	GitMirrorsPath               string   `cli:"git-mirrors-path" normalize:"filepath"`
	GitMirrorsLockTimeout        int      `cli:"git-mirrors-lock-timeout"`
	GitMirrorsSkipUpdate         bool     `cli:"git-mirrors-skip-update"`
	GitSubmoduleCloneConfig      []string `cli:"git-submodule-clone-config"`
	BinPath                      string   `cli:"bin-path" normalize:"filepath"`
	BuildPath                    string   `cli:"build-path" normalize:"filepath"`
	HooksPath                    string   `cli:"hooks-path" normalize:"filepath"`
	PluginsPath                  string   `cli:"plugins-path" normalize:"filepath"`
	CommandEval                  bool     `cli:"command-eval"`
	PluginsEnabled               bool     `cli:"plugins-enabled"`
	PluginValidation             bool     `cli:"plugin-validation"`
	PluginsAlwaysCloneFresh      bool     `cli:"plugins-always-clone-fresh"`
	LocalHooksEnabled            bool     `cli:"local-hooks-enabled"`
	PTY                          bool     `cli:"pty"`
	LogLevel                     string   `cli:"log-level"`
	Debug                        bool     `cli:"debug"`
	Shell                        string   `cli:"shell"`
	Experiments                  []string `cli:"experiment" normalize:"list"`
	Phases                       []string `cli:"phases" normalize:"list"`
	Profile                      string   `cli:"profile"`
	CancelSignal                 string   `cli:"cancel-signal"`
	RedactedVars                 []string `cli:"redacted-vars" normalize:"list"`
	TracingBackend               string   `cli:"tracing-backend"`
	TracingServiceName           string   `cli:"tracing-service-name"`
}

var jobRunFlags = []cli.Flag{
	cli.StringFlag{
		Name:   "command",
		Value:  "",
		Usage:  "The command to run",
		EnvVar: "BUILDKITE_COMMAND",
	},
	cli.StringFlag{
		Name:   "job",
		Value:  "",
		Usage:  "The ID of the job being run",
		EnvVar: "BUILDKITE_JOB_ID",
	},
	cli.StringFlag{
		Name:   "repository",
		Value:  "",
		Usage:  "The repository to clone and run the job from",
		EnvVar: "BUILDKITE_REPO",
	},
	cli.StringFlag{
		Name:   "commit",
		Value:  "",
		Usage:  "The commit to checkout in the repository",
		EnvVar: "BUILDKITE_COMMIT",
	},
	cli.StringFlag{
		Name:   "branch",
		Value:  "",
		Usage:  "The branch the commit is in",
		EnvVar: "BUILDKITE_BRANCH",
	},
	cli.StringFlag{
		Name:   "tag",
		Value:  "",
		Usage:  "The tag the commit",
		EnvVar: "BUILDKITE_TAG",
	},
	cli.StringFlag{
		Name:   "refspec",
		Value:  "",
		Usage:  "Optional refspec to override git fetch",
		EnvVar: "BUILDKITE_REFSPEC",
	},
	cli.StringFlag{
		Name:   "plugins",
		Value:  "",
		Usage:  "The plugins for the job",
		EnvVar: "BUILDKITE_PLUGINS",
	},
	cli.StringFlag{
		Name:   "pullrequest",
		Value:  "",
		Usage:  "The number/id of the pull request this commit belonged to",
		EnvVar: "BUILDKITE_PULL_REQUEST",
	},
	cli.StringFlag{
		Name:   "agent",
		Value:  "",
		Usage:  "The name of the agent running the job",
		EnvVar: "BUILDKITE_AGENT_NAME",
	},
	cli.StringFlag{
		Name:   "queue",
		Value:  "",
		Usage:  "The name of the queue the agent belongs to, if tagged",
		EnvVar: "BUILDKITE_AGENT_META_DATA_QUEUE",
	},
	cli.StringFlag{
		Name:   "organization",
		Value:  "",
		Usage:  "The slug of the organization that the job is a part of",
		EnvVar: "BUILDKITE_ORGANIZATION_SLUG",
	},
	cli.StringFlag{
		Name:   "pipeline",
		Value:  "",
		Usage:  "The slug of the pipeline that the job is a part of",
		EnvVar: "BUILDKITE_PIPELINE_SLUG",
	},
	cli.StringFlag{
		Name:   "pipeline-provider",
		Value:  "",
		Usage:  "The id of the SCM provider that the repository is hosted on",
		EnvVar: "BUILDKITE_PIPELINE_PROVIDER",
	},
	cli.StringFlag{
		Name:   "artifact-upload-paths",
		Value:  "",
		Usage:  "Paths to files to automatically upload at the end of a job",
		EnvVar: "BUILDKITE_ARTIFACT_PATHS",
	},
	cli.StringFlag{
		Name:   "artifact-upload-destination",
		Value:  "",
		Usage:  "A custom location to upload artifact paths to (for example, s3://my-custom-bucket/and/prefix)",
		EnvVar: "BUILDKITE_ARTIFACT_UPLOAD_DESTINATION",
	},
	cli.BoolFlag{
		Name:   "clean-checkout",
		Usage:  "Whether or not the job executor should remove the existing repository before running the command",
		EnvVar: "BUILDKITE_CLEAN_CHECKOUT",
	},
	cli.StringFlag{
		Name:   "git-checkout-flags",
		Value:  "-f",
		Usage:  "Flags to pass to \"git checkout\" command",
		EnvVar: "BUILDKITE_GIT_CHECKOUT_FLAGS",
	},
	cli.StringFlag{
		Name:   "git-clone-flags",
		Value:  "-v",
		Usage:  "Flags to pass to \"git clone\" command",
		EnvVar: "BUILDKITE_GIT_CLONE_FLAGS",
	},
	cli.StringFlag{
		Name:   "git-clone-mirror-flags",
		Value:  "-v",
		Usage:  "Flags to pass to \"git clone\" command when mirroring",
		EnvVar: "BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
	},
	cli.StringFlag{
		Name:   "git-clean-flags",
		Value:  "-ffxdq",
		Usage:  "Flags to pass to \"git clean\" command",
		EnvVar: "BUILDKITE_GIT_CLEAN_FLAGS",
	},
	cli.StringFlag{
		Name:   "git-fetch-flags",
		Value:  "",
		Usage:  "Flags to pass to \"git fetch\" command",
		EnvVar: "BUILDKITE_GIT_FETCH_FLAGS",
	},
	cli.StringSliceFlag{
		Name:   "git-submodule-clone-config",
		Value:  &cli.StringSlice{},
		Usage:  "Comma separated key=value git config pairs applied before git submodule clone commands, e.g. `update --init`. If the config is needed to be applied to all git commands, supply it in a global git config file for the system that the agent runs in instead.",
		EnvVar: "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG",
	},
	cli.StringFlag{
		Name:   "git-mirrors-path",
		Value:  "",
		Usage:  "Path to where mirrors of git repositories are stored",
		EnvVar: "BUILDKITE_GIT_MIRRORS_PATH",
	},
	cli.IntFlag{
		Name:   "git-mirrors-lock-timeout",
		Value:  300,
		Usage:  "Seconds to lock a git mirror during clone, should exceed your longest checkout",
		EnvVar: "BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
	},
	cli.BoolFlag{
		Name:   "git-mirrors-skip-update",
		Usage:  "Skip updating the Git mirror",
		EnvVar: "BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
	},
	cli.StringFlag{
		Name:   "bin-path",
		Value:  "",
		Usage:  "Directory where the buildkite-agent binary lives",
		EnvVar: "BUILDKITE_BIN_PATH",
	},
	cli.StringFlag{
		Name:   "build-path",
		Value:  "",
		Usage:  "Directory where builds will be created",
		EnvVar: "BUILDKITE_BUILD_PATH",
	},
	cli.StringFlag{
		Name:   "hooks-path",
		Value:  "",
		Usage:  "Directory where the hook scripts are found",
		EnvVar: "BUILDKITE_HOOKS_PATH",
	},
	cli.StringFlag{
		Name:   "plugins-path",
		Value:  "",
		Usage:  "Directory where the plugins are saved to",
		EnvVar: "BUILDKITE_PLUGINS_PATH",
	},
	cli.BoolTFlag{
		Name:   "command-eval",
		Usage:  "Allow running of arbitrary commands",
		EnvVar: "BUILDKITE_COMMAND_EVAL",
	},
	cli.BoolTFlag{
		Name:   "plugins-enabled",
		Usage:  "Allow plugins to be run",
		EnvVar: "BUILDKITE_PLUGINS_ENABLED",
	},
	cli.BoolFlag{
		Name:   "plugin-validation",
		Usage:  "Validate plugin configuration",
		EnvVar: "BUILDKITE_PLUGIN_VALIDATION",
	},
	cli.BoolFlag{
		Name:   "plugins-always-clone-fresh",
		Usage:  "Always make a new clone of plugin source, even if already present",
		EnvVar: "BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH",
	},
	cli.BoolTFlag{
		Name:   "local-hooks-enabled",
		Usage:  "Allow local hooks to be run",
		EnvVar: "BUILDKITE_LOCAL_HOOKS_ENABLED",
	},
	cli.BoolTFlag{
		Name:   "ssh-keyscan",
		Usage:  "Automatically run ssh-keyscan before checkout",
		EnvVar: "BUILDKITE_SSH_KEYSCAN",
	},
	cli.BoolTFlag{
		Name:   "git-submodules",
		Usage:  "Enable git submodules",
		EnvVar: "BUILDKITE_GIT_SUBMODULES",
	},
	cli.BoolTFlag{
		Name:   "pty",
		Usage:  "Run jobs within a pseudo terminal",
		EnvVar: "BUILDKITE_PTY",
	},
	cli.StringFlag{
		Name:   "shell",
		Usage:  "The shell to use to interpret build commands",
		EnvVar: "BUILDKITE_SHELL",
		Value:  DefaultShell(),
	},
	cli.StringSliceFlag{
		Name:   "phases",
		Usage:  "The specific phases to execute. The order they're defined is irrelevant.",
		EnvVar: "BUILDKITE_BOOTSTRAP_PHASES,BUILDKITE_JOB_RUN_PHASES",
	},
	cli.StringFlag{
		Name:   "cancel-signal",
		Usage:  "The signal to use for cancellation",
		EnvVar: "BUILDKITE_CANCEL_SIGNAL",
		Value:  "SIGTERM",
	},
	cli.StringSliceFlag{
		Name:   "redacted-vars",
		Usage:  "Pattern of environment variable names containing sensitive values",
		EnvVar: "BUILDKITE_REDACTED_VARS",
	},
	cli.StringFlag{
		Name:   "tracing-backend",
		Usage:  "The name of the tracing backend to use.",
		EnvVar: "BUILDKITE_TRACING_BACKEND",
		Value:  "",
	},
	cli.StringFlag{
		Name:   "tracing-service-name",
		Usage:  "Service name to use when reporting traces.",
		EnvVar: "BUILDKITE_TRACING_SERVICE_NAME",
		Value:  "buildkite-agent",
	},
	DebugFlag,
	LogLevelFlag,
	ExperimentsFlag,
	ProfileFlag,
}

func jobRunAction(c *cli.Context) {
	// The configuration will be loaded into this struct
	cfg := JobRunConfig{}

	loader := cliconfig.Loader{CLI: c, Config: &cfg}
	warnings, err := loader.Load()
	if err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}

	l := CreateLogger(&cfg)

	// Now that we have a logger, log out the warnings that loading config generated
	for _, warning := range warnings {
		l.Warn("%s", warning)
	}

	// Enable experiments
	for _, name := range cfg.Experiments {
		experiments.Enable(name)
	}

	// Handle profiling flag
	done := HandleProfileFlag(l, cfg)
	defer done()

	// Turn of PTY support if we're on Windows
	runInPty := cfg.PTY
	if runtime.GOOS == "windows" {
		runInPty = false
	}

	// Validate phases
	for _, phase := range cfg.Phases {
		switch phase {
		case "plugin", "checkout", "command":
			// Valid phase
		default:
			l.Fatal("Invalid phase %q", phase)
		}
	}

	cancelSig, err := process.ParseSignal(cfg.CancelSignal)
	if err != nil {
		l.Fatal("Failed to parse cancel-signal: %v", err)
	}

	// Configure the job executor
	executor := job.NewExecutor(job.Config{
		AgentName:                    cfg.AgentName,
		ArtifactUploadDestination:    cfg.ArtifactUploadDestination,
		AutomaticArtifactUploadPaths: cfg.AutomaticArtifactUploadPaths,
		BinPath:                      cfg.BinPath,
		Branch:                       cfg.Branch,
		BuildPath:                    cfg.BuildPath,
		CancelSignal:                 cancelSig,
		CleanCheckout:                cfg.CleanCheckout,
		Command:                      cfg.Command,
		CommandEval:                  cfg.CommandEval,
		Commit:                       cfg.Commit,
		Debug:                        cfg.Debug,
		GitCheckoutFlags:             cfg.GitCheckoutFlags,
		GitCleanFlags:                cfg.GitCleanFlags,
		GitCloneFlags:                cfg.GitCloneFlags,
		GitCloneMirrorFlags:          cfg.GitCloneMirrorFlags,
		GitFetchFlags:                cfg.GitFetchFlags,
		GitMirrorsLockTimeout:        cfg.GitMirrorsLockTimeout,
		GitMirrorsPath:               cfg.GitMirrorsPath,
		GitMirrorsSkipUpdate:         cfg.GitMirrorsSkipUpdate,
		GitSubmodules:                cfg.GitSubmodules,
		GitSubmoduleCloneConfig:      cfg.GitSubmoduleCloneConfig,
		HooksPath:                    cfg.HooksPath,
		JobID:                        cfg.JobID,
		LocalHooksEnabled:            cfg.LocalHooksEnabled,
		OrganizationSlug:             cfg.OrganizationSlug,
		Phases:                       cfg.Phases,
		PipelineProvider:             cfg.PipelineProvider,
		PipelineSlug:                 cfg.PipelineSlug,
		PluginValidation:             cfg.PluginValidation,
		Plugins:                      cfg.Plugins,
		PluginsEnabled:               cfg.PluginsEnabled,
		PluginsAlwaysCloneFresh:      cfg.PluginsAlwaysCloneFresh,
		PluginsPath:                  cfg.PluginsPath,
		PullRequest:                  cfg.PullRequest,
		Queue:                        cfg.Queue,
		RedactedVars:                 cfg.RedactedVars,
		RefSpec:                      cfg.RefSpec,
		Repository:                   cfg.Repository,
		RunInPty:                     runInPty,
		SSHKeyscan:                   cfg.SSHKeyscan,
		Shell:                        cfg.Shell,
		Tag:                          cfg.Tag,
		TracingBackend:               cfg.TracingBackend,
		TracingServiceName:           cfg.TracingServiceName,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)
	defer signal.Stop(signals)

	var (
		cancelled bool
		received  os.Signal
		signalMu  sync.Mutex
	)

	// Listen for signals in the background and cancel the job execution
	go func() {
		sig := <-signals
		signalMu.Lock()
		defer signalMu.Unlock()

		// Cancel the job execution
		executor.Cancel()

		// Track the state and signal used
		cancelled = true
		received = sig

		// Remove our signal handler so subsequent signals kill
		signal.Stop(signals)
	}()

	// Run the job and get the exit code
	exitCode := executor.Run(ctx)

	signalMu.Lock()
	defer signalMu.Unlock()

	// If cancelled and our child process returns a non-zero, we should terminate
	// ourselves with the same signal so that our caller can detect and handle appropriately
	if cancelled && runtime.GOOS != "windows" {
		p, err := os.FindProcess(os.Getpid())
		if err != nil {
			l.Error("Failed to find current process: %v", err)
		}

		l.Debug("Terminating job execution after cancellation with %v", received)
		err = p.Signal(received)
		if err != nil {
			l.Error("Failed to signal self: %v", err)
		}
	}

	os.Exit(exitCode)
}

func genBootstrap() cli.Command {
	var help strings.Builder
	help.WriteString("⚠️ ⚠️ ⚠️\n")
	help.WriteString("DEPRECATED: Use `buildkite-agent job run` instead\n")
	help.WriteString("⚠️ ⚠️ ⚠️\n\n")

	err := jobRunHelpTpl.Execute(&help, "bootstrap")
	if err != nil {
		// This can only hapen if we've mangled the template or its parsing
		// and will be caught by tests and local usage
		// (famous last words)
		panic(err)
	}

	return cli.Command{
		Name:        "bootstrap",
		Usage:       "[DEPRECATED] Run a Buildkite job locally",
		Description: help.String(),
		Flags:       jobRunFlags,
		Action: func(c *cli.Context) {
			fmt.Println("⚠️ WARNING ⚠️")
			fmt.Println("This command (`buildkite-agent bootstrap`) is deprecated and will be removed in the next major version of the Buildkite Agent")
			fmt.Println()
			fmt.Println("You're probably seeing this message because you're using the `--bootstrap-script` flag (or its associated environment variable) and manually calling `buildkite-agent bootstrap` from your custom bootstrap script, customising the behaviour of the agent when it runs a job")
			fmt.Println()
			fmt.Println("This workflow is still totally supported, but we've renamed the command to `buildkite-agent job-run` to make it more obvious what it does")
			fmt.Println("You can update your bootstrap script to use `buildkite-agent job run` instead of `buildkite-agent bootstrap` and everything will work pretty much exactly the same")
			fmt.Println("Also, the `--bootstrap-script` flag is now called `--job-run-script`, but the change is backwards compatible -- the old flag will still work for now")
			fmt.Println()
			fmt.Println("For more information, see https://github.com/buildkite/agent/pull/1958")
			fmt.Println()
			jobRunAction(c)
		},
	}
}

func genJobRun() cli.Command {
	var help strings.Builder
	err := jobRunHelpTpl.Execute(&help, "job run")
	if err != nil {
		// This can only hapen if we've mangled the template or its parsing
		// and will be caught by tests and local usage
		// (famous last words)
		panic(err)
	}

	return cli.Command{
		Name:        "run",
		Usage:       "Run a Buildkite job locally",
		Description: help.String(),
		Flags:       jobRunFlags,
		Action:      jobRunAction,
	}
}

var (
	BootstrapCommand = genBootstrap()
	JobRunCommand    = genJobRun()
)
