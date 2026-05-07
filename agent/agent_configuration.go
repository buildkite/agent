package agent

import (
	"regexp"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/job"
)

// AgentConfiguration is the run-time configuration for an agent that
// has been loaded from the config file and command-line params
type AgentConfiguration struct {
	ConfigPath                      string
	BootstrapScript                 string
	BuildPath                       string
	HooksPath                       string
	AdditionalHooksPaths            []string
	SocketsPath                     string
	GitMirrorsPath                  string
	GitMirrorCheckoutMode           string
	GitMirrorsLockTimeout           int
	GitMirrorsSkipUpdate            bool
	PluginsPath                     string
	GitCheckoutFlags                string
	GitCheckoutTimeout              int
	GitCloneFlags                   string
	GitCloneMirrorFlags             string
	GitCleanFlags                   string
	GitCommitVerification           string
	GitFetchFlags                   string
	GitSparseCheckoutPaths          []string
	GitSparseCheckoutMode           job.SparseCheckoutMode
	GitSubmodules                   bool
	GitSubmoduleCloneConfig         []string
	SkipCheckout                    bool
	GitSkipFetchExistingCommits     bool
	CheckoutOverrideMode            env.CheckoutOverrideMode
	CheckoutAttempts                int
	AllowedRepositories             []*regexp.Regexp
	AllowedPlugins                  []*regexp.Regexp
	AllowedEnvironmentVariables     []*regexp.Regexp
	SSHKeyscan                      bool
	CommandEval                     bool
	PluginsEnabled                  bool
	PluginValidation                bool
	PluginsAlwaysCloneFresh         bool
	LocalHooksEnabled               bool
	StrictSingleHooks               bool
	RunInPty                        bool
	KubernetesExec                  bool
	KubernetesContainerStartTimeout time.Duration
	JobContextDir                   string

	SigningJWKSFile  string // Where to find the key to sign pipeline uploads with (passed through to jobs, they might be uploading pipelines)
	SigningJWKSKeyID string // The key ID to sign pipeline uploads with
	SigningAWSKMSKey string // The KMS key ID to sign pipeline uploads with
	SigningGCPKMSKey string // The GCP KMS key to sign pipeline uploads with
	DebugSigning     bool   // Whether to print step payloads when signing them

	VerificationJWKS             any    // The set of keys to verify jobs with
	VerificationFailureBehaviour string // What to do if job verification fails (one of `block` or `warn`)

	HealthCheckAddr             string
	DisconnectAfterJob          bool
	DisconnectAfterIdleTimeout  time.Duration
	DisconnectAfterUptime       time.Duration
	CancelSignalTimeout         time.Duration
	CancelCleanupTimeout        time.Duration
	EnableJobLogTmpfile         bool
	JobLogPath                  string
	WriteJobLogsToStdout        bool
	JobLogsOTLP                 bool
	LogFormat                   string
	Shell                       string
	HooksShell                  string
	Profile                     string
	RedactedVars                []string
	AcquireJob                  string
	TracingBackend              string
	TracingServiceName          string
	TracingPropagateTraceparent bool
	// ControlPlaneTracingExporter is an OTLP trace exporter destination
	// supplied by the control plane at registration (see
	// ApplyControlPlaneTracing). It is delivered to the bootstrap process
	// environment via the standard OTEL_EXPORTER_OTLP_TRACES_* variables and
	// inherited by hooks and the job command, so job-level OTel tooling can
	// export spans to the same collector. Its headers may hold credentials,
	// which is why it is injected after the job env files are written (kept
	// off disk) and skipped when the job env chose its own OTLP destination.
	ControlPlaneTracingExporter  *api.TracingExporter
	DisableWarningsFor           []string
	AllowMultipartArtifactUpload bool
	ArtifactUploadConcurrency    int

	PingMode string
}
