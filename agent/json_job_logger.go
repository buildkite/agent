package agent

import (
	"fmt"
	"log/slog"
	"strings"
)

// JSONJobLogger is a wrapper around a JSON Logger that satisfies the
// io.Writer interface so it can be seamlessly used with existing job logging code.
type JSONJobLogger struct {
	log *slog.Logger
}

// NewJSONJobLogger creates a JSONJobLogger for the given job config, extracting
// structured fields (org, pipeline, branch, etc.) from the job environment.
//
// Output is written to conf.AgentStdout — i.e. the JOB's own log output stream
// that becomes Buildkite log chunks; this is intentionally NOT the agent's
// diagnostic logger.
func NewJSONJobLogger(conf JobRunnerConfig) JSONJobLogger {
	stdout := conf.AgentStdout
	job := conf.Job

	attrs := []any{
		"org", job.Env["BUILDKITE_ORGANIZATION_SLUG"],
		"pipeline", job.Env["BUILDKITE_PIPELINE_SLUG"],
		"branch", job.Env["BUILDKITE_BRANCH"],
		"queue", job.Env["BUILDKITE_AGENT_META_DATA_QUEUE"],
		"build_id", job.Env["BUILDKITE_BUILD_ID"],
		"build_number", job.Env["BUILDKITE_BUILD_NUMBER"],
		"job_url", fmt.Sprintf("%s#%s", job.Env["BUILDKITE_BUILD_URL"], job.ID),
		"build_url", job.Env["BUILDKITE_BUILD_URL"],
		"job_id", job.ID,
		"step_key", job.Env["BUILDKITE_STEP_KEY"],
	}

	// If the job has a W3C traceparent (format: version-trace_id-span_id-flags),
	// include trace_id and span_id in the log fields for trace correlation.
	if parts := strings.SplitN(job.TraceParent, "-", 5); len(parts) >= 4 {
		attrs = append(attrs, "trace_id", parts[1], "span_id", parts[2])
	}

	h := slog.NewJSONHandler(stdout, nil)
	l := slog.New(h).With("source", "job").With(attrs...)

	return JSONJobLogger{log: l}
}

// Write adapts the underlying JSON logger to match the io.Writer interface to
// easier slotting into job logger code. This will write existing fields
// attached to the logger, the message, and write out to the INFO level.
func (l JSONJobLogger) Write(data []byte) (int, error) {
	// When writing as a structured log, trailing newlines and carriage returns
	// generally don't make sense.
	msg := strings.TrimRight(string(data), "\r\n")
	l.log.Info(msg)
	return len(data), nil
}
