package agent

type AgentConfiguration struct {
	BootstrapScript            string
	BuildPath                  string
	HooksPath                  string
	PluginsPath                string
	SSHFingerprintVerification bool
	CommandEval                bool
	RunInPty                   bool
}
