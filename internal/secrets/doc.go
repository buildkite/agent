// Package secrets provides internal functionality for fetching and processing
// secrets from the Buildkite API during job execution.
//
// This package is for internal use by the Buildkite agent only and should not
// be imported by external code. The API may change without notice.
//
// The package provides:
//   - Manager: orchestrates secret fetching and processing
//   - Processor interface: extensible secret type handling
//   - EnvironmentVariableProcessor: sets secrets as environment variables
//
// Usage:
//
//	manager := secrets.NewManager(apiClient)
//	processors := []secrets.Processor{&secrets.EnvironmentVariableProcessor{Env: env, Redactors: redactors}}
//	err := manager.FetchAndProcess(ctx, jobID, secrets, processors)
package secrets
