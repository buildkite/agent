package agent

type AgentConfiguration struct {
	BootstrapScript            string
	BuildPath                  string
	HooksPath                  string
	SSHFingerprintVerification bool
	CommandEval                bool
	RunInPty                   bool
}
