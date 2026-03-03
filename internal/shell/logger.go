package shell

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"slices"
	"testing"
)

// Logger represents a logger that outputs to a buildkite shell.
type Logger interface {
	io.Writer

	// Printf prints a line of output
	Printf(format string, v ...any)

	// Headerf prints a Buildkite formatted header
	Headerf(format string, v ...any)

	// Commentf prints a comment line, e.g `# my comment goes here`
	Commentf(format string, v ...any)

	// Errorf shows a Buildkite formatted error expands the previous group
	Errorf(format string, v ...any)

	// Warningf shows a buildkite bootstrap warning
	Warningf(format string, v ...any)

	// OptionalWarningf shows a warning, but only if it hasn't been explicitly disabled by the user
	OptionalWarningf(id, format string, v ...any)

	// Promptf prints a shell prompt
	Promptf(format string, v ...any)
}

// StderrLogger is a Logger that writes to Stderr
var StderrLogger = &WriterLogger{
	Writer: os.Stderr,
	Ansi:   true,
}

// DiscardLogger discards all log messages
var DiscardLogger = &WriterLogger{
	Writer: io.Discard,
}

// WriterLogger provides a logger that writes to an io.Writer
type WriterLogger struct {
	Writer             io.Writer
	Ansi               bool
	DisabledWarningIDs []string
}

func NewWriterLogger(writer io.Writer, ansi bool, disabledWarningIDs []string) *WriterLogger {
	return &WriterLogger{
		Writer:             writer,
		Ansi:               ansi,
		DisabledWarningIDs: disabledWarningIDs,
	}
}

func (wl *WriterLogger) Write(b []byte) (int, error) {
	wl.Printf("%s", b)
	return len(b), nil
}

func (wl *WriterLogger) Printf(format string, v ...any) {
	fmt.Fprintf(wl.Writer, format+"\n", v...) //nolint:errcheck // logger output; error handling would recurse
}

func (wl *WriterLogger) Headerf(format string, v ...any) {
	fmt.Fprintf(wl.Writer, "~~~ "+format+"\n", v...) //nolint:errcheck // logger output; error handling would recurse
}

func (wl *WriterLogger) Commentf(format string, v ...any) {
	if wl.Ansi {
		wl.Printf(ansiColor("# "+format, "90"), v...)
	} else {
		wl.Printf("# "+format, v...)
	}
}

func (wl *WriterLogger) Errorf(format string, v ...any) {
	if wl.Ansi {
		wl.Printf(ansiColor("🚨 Error: "+format, "31")+"\n^^^ +++", v...)
	} else {
		wl.Printf("🚨 Error: "+format+"\n^^^ +++", v...)
	}
}

func (wl *WriterLogger) Warningf(format string, v ...any) {
	if wl.Ansi {
		wl.Printf(ansiColor("⚠️ Warning: "+format, "33")+"\n^^^ +++", v...)
	} else {
		wl.Printf("⚠️ Warning: "+format+"\n^^^ +++", v...)
	}
}

func (wl *WriterLogger) OptionalWarningf(id, format string, v ...any) {
	if slices.Contains(wl.DisabledWarningIDs, id) {
		wl.Printf(format, v...)
		return
	}

	warningFormatWithDisable := format + fmt.Sprintf(". You can disable this warning by passing the `--disable-warnings-for %s` flag", id)
	wl.Warningf(warningFormatWithDisable, v...)
}

func (wl *WriterLogger) Promptf(format string, v ...any) {
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	if wl.Ansi {
		wl.Printf(ansiColor(prompt, "90")+" "+format, v...)
	} else {
		wl.Printf(prompt+" "+format, v...)
	}
}

func ansiColor(s, attributes string) string {
	return fmt.Sprintf("\033[%sm%s\033[0m", attributes, s)
}

type TestingLogger struct {
	*testing.T
}

func (tl TestingLogger) Write(b []byte) (int, error) {
	tl.Logf("%s", b)
	return len(b), nil
}

func (tl TestingLogger) Printf(format string, v ...any) {
	tl.Logf(format, v...)
}

func (tl TestingLogger) Headerf(format string, v ...any) {
	tl.Logf("~~~ "+format, v...)
}

func (tl TestingLogger) Commentf(format string, v ...any) {
	tl.Logf("# "+format, v...)
}

func (tl TestingLogger) Errorf(format string, v ...any) {
	tl.Logf("🚨 Error: "+format, v...)
}

func (tl TestingLogger) Warningf(format string, v ...any) {
	tl.Logf("⚠️ Warning: "+format, v...)
}

func (tl TestingLogger) OptionalWarningf(_id, format string, v ...any) {
	// We don't care about optionality for test logging
	tl.Warningf(format, v...)
}

func (tl TestingLogger) Promptf(format string, v ...any) {
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	tl.Logf(prompt+" "+format, v...)
}

type LoggerStreamer struct {
	Logger  Logger
	Prefix  string
	started bool
	buf     *bytes.Buffer
	offset  int
}

var lineRegexp = regexp.MustCompile(`(?m:^(.*)\r?\n)`)

func NewLoggerStreamer(logger Logger) *LoggerStreamer {
	return &LoggerStreamer{
		Logger: logger,
		buf:    bytes.NewBuffer(nil),
	}
}

func (l *LoggerStreamer) Write(p []byte) (n int, err error) {
	if bytes.ContainsRune(p, '\n') {
		l.started = true
	}

	if n, err = l.buf.Write(p); err != nil {
		return n, err
	}

	err = l.Output()
	return n, err
}

func (l *LoggerStreamer) Close() error {
	if remaining := l.buf.String()[l.offset:]; len(remaining) > 0 {
		l.Logger.Printf("%s%s", l.Prefix, remaining)
	}
	l.buf = bytes.NewBuffer(nil)
	return nil
}

func (l *LoggerStreamer) Output() error {
	if !l.started {
		return nil
	}

	matches := lineRegexp.FindAllStringSubmatch(l.buf.String()[l.offset:], -1)

	for _, match := range matches {
		l.Logger.Printf("%s%s", l.Prefix, match[1])
		l.offset += len(match[0])
	}

	return nil
}
