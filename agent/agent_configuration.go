package agent

import (
	"regexp"
	"time"
)

// AgentConfiguration is the run-time configuration for an agent that
// has been loaded from the config file and command-line params
type AgentConfiguration struct {
	ConfigPath                  string
	BootstrapScript             string
	BuildPath                   string
	HooksPath                   string
	AdditionalHooksPaths        []string
	SocketsPath                 string
	GitMirrorsPath              string
	GitMirrorsLockTimeout       int
	GitMirrorsSkipUpdate        bool
	PluginsPath                 string
	GitCheckoutFlags            string
	GitCloneFlags               string
	GitCloneMirrorFlags         string
	GitCleanFlags               string
	GitFetchFlags               string
	GitSubmodules               bool
	SkipCheckout                bool
	AllowedRepositories         []*regexp.Regexp
	AllowedPlugins              []*regexp.Regexp
	AllowedEnvironmentVariables []*regexp.Regexp
	SSHKeyscan                  bool
	CommandEval                 bool
	PluginsEnabled              bool
	PluginValidation            bool
	PluginsAlwaysCloneFresh     bool
	LocalHooksEnabled           bool
	StrictSingleHooks           bool
	RunInPty                    bool
	KubernetesExec              bool

	SigningJWKSFile  string // Where to find the key to sign pipeline uploads with (passed through to jobs, they might be uploading pipelines)
	SigningJWKSKeyID string // The key ID to sign pipeline uploads with
	SigningAWSKMSKey string // The KMS key ID to sign pipeline uploads with
	DebugSigning     bool   // Whether to print step payloads when signing them

	VerificationJWKS             any    // The set of keys to verify jobs with
	VerificationFailureBehaviour string // What to do if job verification fails (one of `block` or `warn`)

	ANSITimestamps               bool
	TimestampLines               bool
	HealthCheckAddr              string
	DisconnectAfterJob           bool
	DisconnectAfterIdleTimeout   time.Duration
	DisconnectAfterUptime        time.Duration
	CancelGracePeriod            int
	SignalGracePeriod            time.Duration
	EnableJobLogTmpfile          bool
	JobLogPath                   string
	WriteJobLogsToStdout         bool
	LogFormat                    string
	Shell                        string
	Profile                      string
	RedactedVars                 []string
	AcquireJob                   string
	TracingBackend               string
	TracingServiceName           string
	TracingPropagateTraceparent  bool
	TraceContextEncoding         string
	DisableWarningsFor           []string
	AllowMultipartArtifactUpload bool
}
