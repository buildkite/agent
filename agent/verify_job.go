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
		r.logger.Warn("Job will be run whether or not it can be verified - this is not recommended. You can change this behavior with the `job-verification-failure-behavior` agent configuration option.")
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
	// - env vars are complicated
	// - plugins should match BUILDKITE_PLUGINS semantically
	// - matrix interpolates into the other fields, and does not itself compare
	//   (not yet implemented).
	//
	// We can't check that the job is consistent with fields we don't know
	// about yet, so these are rejected.
	//
	// Notes on env vars:
	// 1. Pipeline env values are included individually in the "env::" namespace
	//    of fields, and step env vars are a single map under the "env" field.
	// 2. When producing the job env, step env overrides pipeline env. Easy.
	// 3. The backend also adds env vars that can't be known in advance for
	//    signing.
	// 4. Step env vars can have matrix tokens (in both names and values).
	// 5. Pipeline env is uploaded as a distinct map, but is not fully available
	//    for verifying here - we only have job env and step env to work with,
	//    and only know which vars were pipeline env vars from "env::".
	//
	// As a result, the job env, pipeline env, and step env can all disagree
	// under normal circumstances, and verifying it all is a bit complex.
	//
	// 1. Every step env var must at least exist in the job env.
	// 2. Every pipline env var must at least exist in the job env.
	// 3. If a var was a pipeline env var, and wasn't overridden by a step var,
	//    it went in the "env::" namespace, and Signature.Verify has checked its
	//    value implicitly.
	// 3. If a var was a step env var, it is an element in step.Env and
	//    Signature.Verify has checked its pre-matrix value.
	// 4. If a var was a step env var then its post-matrix value must equal
	//    the job env var.
	signedFields := step.Signature.SignedFields

	// Compare each field to the job.
	for _, field := range signedFields {
		switch field {
		case "command": // compare directly
			jobCommand := r.conf.Job.Env["BUILDKITE_COMMAND"]
			if step.Command != jobCommand {
				return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of BUILDKITE_COMMAND (%q) does not match the value of step.command (%q)", r.conf.Job.ID, step.Signature.Value, jobCommand, step.Command))
			}

		case "env":
			// Everything in the step env (post-matrix interpolation) must be
			// present in the job env, and have equal values.
			for name, stepEnvValue := range step.Env {
				jobEnvValue, has := r.conf.Job.Env[name]
				if !has {
					return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but step.env defines %s which is missing from the job environment", r.conf.Job.ID, step.Signature.Value, name))
				}
				if jobEnvValue != stepEnvValue {
					return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of %s (%q) does not match the value of step.env[%s] (%q)", r.conf.Job.ID, step.Signature.Value, name, jobEnvValue, name, stepEnvValue))
				}
			}

		case "plugins": // compare canonicalised JSON
			jobPluginsJSON := r.conf.Job.Env["BUILDKITE_PLUGINS"]
			// Various equivalent ways to represent "no plugins", however...
			// jcs.Transform chokes on "" and "null", and json.Marshal encodes
			// nil slice as "null", but zero-length slice as "[]".
			emptyStepPlugins := len(step.Plugins) == 0
			emptyJobPlugins := (jobPluginsJSON == "" || jobPluginsJSON == "null" || jobPluginsJSON == "[]")

			if emptyStepPlugins && emptyJobPlugins {
				continue // both empty
			}

			// Marshal step.Plugins into JSON now, in case it is needed for the
			// error.
			stepPluginsJSON, err := json.Marshal(step.Plugins)
			if err != nil {
				return fmt.Errorf("marshaling step plugins JSON: %v", err)
			}

			if emptyStepPlugins != emptyJobPlugins {
				// one is empty but the other is not
				return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of BUILDKITE_PLUGINS (%q) does not match the value of step.plugins (%q)", r.conf.Job.ID, step.Signature.Value, jobPluginsJSON, stepPluginsJSON))
			}

			stepPluginsNorm, err := jcs.Transform(stepPluginsJSON)
			if err != nil {
				return fmt.Errorf("canonicalising step plugins JSON: %v", err)
			}
			jobPluginsNorm, err := jcs.Transform([]byte(jobPluginsJSON))
			if err != nil {
				return fmt.Errorf("canonicalising BUILDKITE_PLUGINS: %v", err)
			}

			if !bytes.Equal(jobPluginsNorm, stepPluginsNorm) {
				return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but the value of BUILDKITE_PLUGINS (%q) does not match the value of step.plugins (%q)", r.conf.Job.ID, step.Signature.Value, jobPluginsNorm, stepPluginsNorm))
			}

		case "matrix": // compared indirectly through other fields
			continue

		default:
			// env:: - skip any that were verified with Verify.
			if name, isEnv := strings.CutPrefix(field, pipeline.EnvNamespacePrefix); isEnv {
				if _, has := r.conf.Job.Env[name]; !has {
					// A pipeline env var that is now missing.
					return newInvalidSignatureError(fmt.Errorf("job %q was signed with signature %q, but pipeline.env defines %s which is missing from the job environment", r.conf.Job.ID, step.Signature.Value, name))
				}
				// The env var is present. Signature.Verify used the value from
				// the job env, handling this case.
				continue
			}

			// We don't know this field, so we cannot ensure it is consistent
			// with the job.
			return fmt.Errorf("unknown or unsupported field %q on Job struct for signing/verification of job %s", field, r.conf.Job.ID)
		}
	}

	return nil
}
