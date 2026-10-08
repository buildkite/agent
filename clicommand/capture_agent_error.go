package clicommand

import (
	"context"
	"os"
	"time"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/buildkite/agent/v4/logger"
)

// captureAgentError reports a failure of this command as a job error when the
// job opted in with BUILDKITE_CAPTURE_AGENT_ERRORS and runs with a Local Job
// API that can capture errors. The Local Job API redacts registered secrets.
// Delivery is best-effort and never changes the command's result.
func captureAgentError(ctx context.Context, l logger.Logger, code, message string) {
	if os.Getenv("BUILDKITE_CAPTURE_AGENT_ERRORS") != "true" ||
		os.Getenv("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR") != "true" ||
		ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client, err := jobapi.NewDefaultClient(ctx)
	if err == nil {
		_, err = client.CaptureError(ctx, &jobapi.CapturedError{Code: code, Message: message})
	}
	if err != nil {
		// Transport errors can contain upstream response bodies or socket paths.
		l.Warnf("Could not capture job error %q", code)
	}
}

// storageErrorText describes an artifact or cache storage failure without URL
// query strings, which can hold the signature of a presigned URL. The Local Job
// API masks URL credentials and registered secrets.
func storageErrorText(err error) string {
	return redact.URLQueriesInText(err.Error())
}
