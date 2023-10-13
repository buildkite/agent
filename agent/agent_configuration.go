package agent

import (
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

// AgentConfiguration is the run-time configuration for an agent that
// has been loaded from the config file and command-line params
type AgentConfiguration struct {
	ConfigPath            string
	BootstrapScript       string
	BuildPath             string
	HooksPath             string
	SocketsPath           string
	GitMirrorsPath        string
	GitMirrorsLockTimeout int
	GitMirrorsSkipUpdate  bool
	PluginsPath           string
	GitCheckoutFlags      string
	GitCloneFlags         string
	GitCloneMirrorFlags   string
	GitCleanFlags         string
	GitFetchFlags         string
	GitSubmodules         bool
	AllowedRepositories   []string
	CommandMode           string
	CommandRepoOnly       bool
	SSHKeyscan            bool
	CommandEval           bool
	PluginsEnabled        bool
	PluginValidation      bool
	LocalHooksEnabled     bool
	StrictSingleHooks     bool
	RunInPty              bool

	JobSigningJWKSPath string // Where to find the key to sign jobs with (passed through to jobs, they might be uploading pipelines)
	JobSigningKeyID    string // The key ID to sign jobs with

	JobVerificationJWKS             jwk.Set // The set of keys to verify jobs with
	JobVerificationFailureBehaviour string  // What to do if job verification fails (one of `block` or `warn`)

	ANSITimestamps             bool
	TimestampLines             bool
	HealthCheckAddr            string
	DisconnectAfterJob         bool
	DisconnectAfterIdleTimeout int
	CancelGracePeriod          int
	SignalGracePeriod          time.Duration
	EnableJobLogTmpfile        bool
	JobLogPath                 string
	WriteJobLogsToStdout       bool
	LogFormat                  string
	Shell                      string
	Profile                    string
	RedactedVars               []string
	AcquireJob                 string
	TracingBackend             string
	TracingServiceName         string
}
