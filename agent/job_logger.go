package agent

import "io"

// JobLogger satisfies io.Writer and routes job output either through a
// structured JSON logger or directly to stdout depending on the log format.
type JobLogger struct {
	w io.Writer
}

// NewJobLogger creates a JobLogger for the given config. When LogFormat is
// "json", output is written through a JSONJobLogger with structured fields.
// Otherwise, output is written directly to AgentStdout.
func NewJobLogger(conf JobRunnerConfig) JobLogger {
	if conf.AgentConfiguration.LogFormat == "json" {
		return JobLogger{w: NewJSONJobLogger(conf)}
	}
	return JobLogger{w: conf.AgentStdout}
}

func (l JobLogger) Write(data []byte) (int, error) {
	return l.w.Write(data)
}
