package agent

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
	GitFetchFlags              string
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
