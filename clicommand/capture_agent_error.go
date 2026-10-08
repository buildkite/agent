package clicommand

import (
	"context"
	"errors"
	"fmt"
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

// storageErrorText describes err for a captured error without URL query
// strings, which can carry presigned credentials.
func storageErrorText(err error) string {
	return redact.URLQueriesInText(err.Error())
}

// uploadErrorText is storageErrorText for a failed artifact upload, which
// joins the errors of each failed file. It describes the first and counts the
// others, so one cause fits in the message.
func uploadErrorText(err error) string {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		if errs := joined.Unwrap(); len(errs) > 1 {
			more := "errors"
			if len(errs) == 2 {
				more = "error"
			}
			return fmt.Sprintf("%s (and %d more %s)", storageErrorText(errs[0]), len(errs)-1, more)
		}
	}
	return storageErrorText(err)
}
