package agent

// AgentConfiguration is the run-time configuration for an agent that
// has been loaded from the config file and command-line params
type AgentConfiguration struct {
	ConfigPath                 string
	JobRunScript               string
	BuildPath                  string
	HooksPath                  string
	SocketsPath                string
	GitMirrorsPath             string
	GitMirrorsLockTimeout      int
	GitMirrorsSkipUpdate       bool
	PluginsPath                string
	GitCheckoutFlags           string
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
	TimestampLines             bool
	HealthCheckAddr            string
	DisconnectAfterJob         bool
	DisconnectAfterIdleTimeout int
	CancelGracePeriod          int
	EnableJobLogTmpfile        bool
	WriteJobLogsToStdout       bool
	LogFormat                  string
	Shell                      string
	Profile                    string
	RedactedVars               []string
	AcquireJob                 string
	TracingBackend             string
	TracingServiceName         string
}
