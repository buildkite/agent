package agent

type AgentConfiguration struct {
	BootstrapScript                string
	BuildPath                      string
	HooksPath                      string
	AutoSSHFingerprintVerification bool
	CommandEval                    bool
	RunInPty                       bool
	ArtifactUnixSyntax             bool
}
