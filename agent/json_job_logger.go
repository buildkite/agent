package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/buildkite/agent/v4/logger"
)

// JSONJobLogger is a wrapper around a JSON Logger that satisfies the
// io.Writer interface so it can be seamlessly used with existing job logging code.
type JSONJobLogger struct {
	log logger.Logger
}

// NewJSONJobLogger creates a JSONJobLogger for the given job config, extracting
// structured fields (org, pipeline, branch, etc.) from the job environment.
func NewJSONJobLogger(conf JobRunnerConfig) JSONJobLogger {
	stdout := conf.AgentStdout
	job := conf.Job

	fields := []logger.Field{
		logger.StringField("org", job.Env["BUILDKITE_ORGANIZATION_SLUG"]),
		logger.StringField("pipeline", job.Env["BUILDKITE_PIPELINE_SLUG"]),
		logger.StringField("branch", job.Env["BUILDKITE_BRANCH"]),
		logger.StringField("queue", job.Env["BUILDKITE_AGENT_META_DATA_QUEUE"]),
		logger.StringField("build_id", job.Env["BUILDKITE_BUILD_ID"]),
		logger.StringField("build_number", job.Env["BUILDKITE_BUILD_NUMBER"]),
		logger.StringField("job_url", fmt.Sprintf("%s#%s", job.Env["BUILDKITE_BUILD_URL"], job.ID)),
		logger.StringField("build_url", job.Env["BUILDKITE_BUILD_URL"]),
		logger.StringField("job_id", job.ID),
		logger.StringField("step_key", job.Env["BUILDKITE_STEP_KEY"]),
	}

	// If the job has a W3C traceparent (format: version-trace_id-span_id-flags),
	// include trace_id and span_id in the log fields for trace correlation.
	if parts := strings.SplitN(job.TraceParent, "-", 5); len(parts) >= 4 {
		fields = append(fields,
			logger.StringField("trace_id", parts[1]),
			logger.StringField("span_id", parts[2]),
		)
	}

	l := logger.NewConsoleLogger(logger.NewJSONPrinter(stdout), os.Exit)
	l = l.WithFields(logger.StringField("source", "job"))
	l = l.WithFields(fields...)

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
