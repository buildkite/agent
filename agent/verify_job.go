package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/go-pipeline/signature"
	"github.com/gowebpki/jcs"
)

var (
	ErrNoSignature        = errors.New("job had no signature to verify")
	ErrVerificationFailed = errors.New("signature verification failed")
	ErrInvalidJob         = errors.New("job does not match signed step")
)

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

func (r *JobRunner) verifyJob(ctx context.Context, keySet any) error {
	step := &r.conf.Job.Step

	// First, verify the signature.
	err := signature.VerifyStep(
		ctx,
		step,
		keySet,
		r.conf.Job.Env["BUILDKITE_REPO"],
		signature.WithEnv(r.conf.Job.Env),
		signature.WithLogger(r.agentLogger),
		signature.WithDebugSigning(r.conf.AgentConfiguration.DebugSigning),
	)
	if err == signature.ErrNoSignature {
		r.agentLogger.Debug("verifyJob: Job.Step.Signature == nil")
		return ErrNoSignature
	}
	if err != nil {
		r.agentLogger.Debug("failed to verifyJob: step.Signature.Verify(Job.Env, stepWithInvariants, JWKS) = %v", err)
		return newInvalidSignatureError(ErrVerificationFailed)
	}

	// Interpolate the matrix permutation (validating the permutation in the
	// process).
	if err := step.InterpolateMatrixPermutation(r.conf.Job.MatrixPermutation); err != nil {
		r.agentLogger.Debug("failed to verifyJob: step.InterpolateMatrixPermutation(% #v) = %v", r.conf.Job.MatrixPermutation, err)
		return newInvalidSignatureError(ErrInvalidJob)
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
	// - matrix interpolates into the others, and does not itself compare
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
	// 4. Step env vars can have matrix tokens (interpolated within values, but
	//    not variable names).
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
				r.agentLogger.Debug("failed to verifyJob: BUILDKITE_COMMAND = %q != %q = step.Command", jobCommand, step.Command)
				return newInvalidSignatureError(ErrInvalidJob)
			}

		case "env":
			// Everything in the step env (post-matrix interpolation) must be
			// present in the job env, and have equal values.
			for name, stepEnvValue := range step.Env {
				jobEnvValue, has := r.conf.Job.Env[name]
				if !has {
					r.agentLogger.Debug("failed to verifyJob: %q missing from Job.Env; step.Env[%q] = %q", name, name, stepEnvValue)
					return newInvalidSignatureError(ErrInvalidJob)
				}
				if jobEnvValue != stepEnvValue {
					r.agentLogger.Debug("failed to verifyJob: Job.Env[%q] = %q != %q = step.Env[%q]", name, jobEnvValue, stepEnvValue, name)
					return newInvalidSignatureError(ErrInvalidJob)
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
				r.agentLogger.Debug("verifyJob: both BUILDKITE_PLUGINS and step.Plugins are empty/null")
				continue // both empty
			}
			if emptyStepPlugins != emptyJobPlugins {
				// one is empty but the other is not
				r.agentLogger.Debug("failed to verifyJob: emptyJobPlugins = %t != %t = emptyStepPlugins", emptyJobPlugins, emptyStepPlugins)
				return newInvalidSignatureError(ErrInvalidJob)
			}

			stepPluginsJSON, err := json.Marshal(step.Plugins)
			if err != nil {
				r.agentLogger.Debug("failed to verifyJob: json.Marshal(step.Plugins) = %v", err)
				return newInvalidSignatureError(ErrInvalidJob)
			}
			stepPluginsNorm, err := jcs.Transform(stepPluginsJSON)
			if err != nil {
				r.agentLogger.Debug("failed to verifyJob: jcs.Transform(stepPluginsJSON) = %v", err)
				return newInvalidSignatureError(ErrInvalidJob)
			}
			jobPluginsNorm, err := jcs.Transform([]byte(jobPluginsJSON))
			if err != nil {
				r.agentLogger.Debug("failed to verifyJob: jcs.Transform(jobPluginsJSON) = %v", err)
				return newInvalidSignatureError(ErrInvalidJob)
			}

			if !bytes.Equal(jobPluginsNorm, stepPluginsNorm) {
				r.agentLogger.Debug("failed to verifyJob: jobPluginsNorm = %q != %q = stepPluginsNorm", jobPluginsNorm, stepPluginsNorm)
				return newInvalidSignatureError(ErrInvalidJob)
			}

		case "matrix": // compared indirectly through other fields
			continue

		case "repository_url":
			// This was sourced from the job itself, not the step, when the signature was verified.
			// So, we don't need to confirm that the values in the job are the same as those in the step.
			continue

		case "secrets":
			jobSecrets := r.conf.Job.Step.Secrets

			if len(step.Secrets) == 0 && len(jobSecrets) == 0 {
				r.agentLogger.Debug("verifyJob: both job.Step.Secrets and step.Secrets are empty")
				continue // both empty
			}

			if len(step.Secrets) != len(jobSecrets) {
				r.agentLogger.Debug("failed to verifyJob: step.Secrets length %d != jobSecrets length %d", len(step.Secrets), len(jobSecrets))
				return newInvalidSignatureError(ErrInvalidJob)
			}

			for i, stepSecret := range step.Secrets {
				jobSecret := jobSecrets[i]

				if stepSecret.Key != jobSecret.Key {
					r.agentLogger.Debug("failed to verifyJob: secret at index %d - Step key %q did not match job key %q", i, stepSecret.Key, jobSecret.Key)
					return newInvalidSignatureError(ErrInvalidJob)
				}

				if stepSecret.EnvironmentVariable != jobSecret.EnvironmentVariable {
					r.agentLogger.Debug("failed to verifyJob: secret at index %d - Step environment variable %q did not match job environment variable %q", i, stepSecret.EnvironmentVariable, jobSecret.EnvironmentVariable)
					return newInvalidSignatureError(ErrInvalidJob)
				}
			}

		default:
			// env:: - skip any that were verified with Verify.
			if name, isEnv := strings.CutPrefix(field, signature.EnvNamespacePrefix); isEnv {
				if _, has := r.conf.Job.Env[name]; !has {
					// A pipeline env var that is now missing.
					r.agentLogger.Debug("failed to verifyJob: %q missing from Job.Env", name)
					return newInvalidSignatureError(ErrInvalidJob)
				}
				// The env var is present. Signature.Verify used the value from
				// the job env, handling this case.
				continue
			}

			// We don't know this field, so we cannot ensure it is consistent
			// with the job.
			r.agentLogger.Debug("failed to verifyJob: mystery signed field %q", field)
			return newInvalidSignatureError(ErrInvalidJob)
		}
	}

	return nil
}
