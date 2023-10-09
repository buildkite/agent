package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/gowebpki/jcs"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

var ErrNoSignature = errors.New("job had no signature to verify")

type invalidSignatureError struct {
	underlying error
}

func newInvalidSignatureError(err error) *invalidSignatureError {
	return &invalidSignatureError{underlying: err}
}

func (e *invalidSignatureError) Error() string {
	return fmt.Sprintf("invalid signature: %v", e.underlying)
}

func (e *invalidSignatureError) Unwrap() error {
	return e.underlying
}

func (r *JobRunner) verificationFailureLogs(err error, behavior string) {
	label := "WARNING"
	if behavior == VerificationBehaviourBlock {
		label = "ERROR"
	}

	r.logger.Warn("Job verification failed: %s", err.Error())
	r.logStreamer.Process([]byte(fmt.Sprintf("⚠️ %s: Job verification failed: %s\n", label, err.Error())))

	if behavior == VerificationBehaviourWarn {
		r.logger.Warn("Job will be run without verification - this is not recommended. You can change this behavior with the `job-verification-no-signature-behavior` agent configuration option.")
		r.logStreamer.Process([]byte(fmt.Sprintf("⚠️ %s: Job will be run without verification\n", label)))
	}
}

func (r *JobRunner) verifyJob(keySet jwk.Set) error {
	step := r.conf.Job.Step

	if step.Matrix != nil {
		r.logger.Warn("Signing/Verification of matrix jobs is not currently supported")
		r.logger.Warn("Watch this space 👀")

		return nil
	}

	if step.Signature == nil {
		return ErrNoSignature
	}

	// Verify the signature
	if err := step.Signature.Verify(r.conf.Job.Env, &step, r.conf.JWKS); err != nil {
		return newInvalidSignatureError(err)
	}

	// Now that the signature of the job's step is verified, we need to check if
	// the fields on the job match those on the step. If they don't, we need to
	// fail the job - more or less the only reason that the job and the step
	// would have different fields would be if someone had modified the job on
	// the backend after it was signed (aka crimes).
	//
	// Note that each field needs a different consistency check:
	//
	// - command should match BUILDKITE_COMMAND exactly
	// - env vars not listed in the signature as signed fields are allowed
	// - plugins should match BUILDKITE_PLUGINS semantically
	// - matrix interpolates into the others, and does not itself compare
	//   (not yet implemented).
	//
	// We can't check that the job is consistent with fields we don't know
	// about yet, so these are rejected.
	//
	// More notes on `env::`:
	// 1. Signature.Verify ensures every signed env var has the right value, and
	// 2. step env can be overridden by the pipeline env, but each step only
	//    knows about its own env. So the job env and step env can disagree
	//    under normal circumstances.
	// We still have to catch when a signature validates only because the step
	// has an env var (not used to run the job), that is not present in the
	// job env (is actually used).
	signedFields := step.Signature.SignedFields

	stepFields, err := step.ValuesForFields(signedFields)
	if err != nil {
		return fmt.Errorf("failed to get values for fields %v on step: %w", signedFields, err)
	}

	for _, field := range signedFields {
		switch field {
		case "command": // compare directly
			if jobCommand := r.conf.Job.Env["BUILDKITE_COMMAND"]; stepFields[field] != jobCommand {
				return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of field %q on the job (%q) does not match the value of the field on the step (%q)", r.conf.Job.ID, step.Signature.Value, field, jobCommand, stepFields[field]))
			}

		case "plugins": //  compare canonicalised JSON
			stepPluginsJSON, err := json.Marshal(stepFields[field])
			if err != nil {
				return fmt.Errorf("marshaling step plugins JSON: %v", err)
			}
			stepPluginsNorm, err := jcs.Transform(stepPluginsJSON)
			if err != nil {
				return fmt.Errorf("canonicalising step plugins JSON: %v", err)
			}
			jobPluginsNorm, err := jcs.Transform([]byte(r.conf.Job.Env["BUILDKITE_PLUGINS"]))
			if err != nil {
				return fmt.Errorf("canonicalising BUILDKITE_PLUGINS: %v", err)
			}

			if !bytes.Equal(jobPluginsNorm, stepPluginsNorm) {
				return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of field %q on the job (%q) does not match the value of the field on the step (%q)", r.conf.Job.ID, step.Signature.Value, field, jobPluginsNorm, stepPluginsNorm))
			}

		default:
			// env:: - skip any that were verified with Verify.
			if envName, ok := strings.CutPrefix(field, pipeline.EnvNamespacePrefix); ok {
				if _, has := r.conf.Job.Env[envName]; has {
					// Signature.Verify used the variable value from Env,
					// handling this case.
					continue
				}

				// It's not in the job env, so ensure that it was blank in the
				// step env too.
				if stepFields[field] != "" {
					return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of field %q on the job (%q) does not match the value of the field on the step (%q)", r.conf.Job.ID, step.Signature.Value, field, "", stepFields[field]))
				}
			}

			// We don't know this field, so we cannot ensure it is consistent
			// with the job.
			return fmt.Errorf("unknown or unsupported field %q on Job struct for signing/verification of job %s", field, r.conf.Job.ID)
		}
	}

	return nil
}
