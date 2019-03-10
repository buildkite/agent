package agent

type AgentConfiguration struct {
	ConfigPath                 string
	BootstrapScript            string
	BuildPath                  string
	HooksPath                  string
	GitMirrorsPath             string
	PluginsPath                string
	GitCloneFlags              string
	GitCleanFlags              string
	GitSubmodules              bool
	SSHKeyscan                 bool
	CommandEval                bool
	PluginsEnabled             bool
	PluginValidation           bool
	LocalHooksEnabled          bool
	RunInPty                   bool
	TimestampLines             bool
	DisconnectAfterJob         bool
	DisconnectAfterJobTimeout  int
	DisconnectAfterIdleTimeout int
	CancelGracePeriod          int
	Shell                      string
}
