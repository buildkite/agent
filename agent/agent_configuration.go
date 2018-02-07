package agent

type AgentConfiguration struct {
	BootstrapScript            string
	BuildPath                  string
	HooksPath                  string
	PluginsPath                string
	GitCloneFlags              string
	GitCleanFlags              string
	SSHFingerprintVerification bool
	CommandEval                bool
	PluginsEnabled             bool
	RunInPty                   bool
	TimestampLines             bool
	DisconnectAfterJob         bool
	DisconnectAfterJobTimeout  int
	Shell                      string
}
