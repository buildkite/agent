package shell

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
)

// Logger represents a logger that outputs to a buildkite shell.
type Logger interface {
	// Printf prints a line of output
	Printf(format string, v ...interface{})

	// Headerf prints a Buildkite formatted header
	Headerf(format string, v ...interface{})

	// Commentf prints a comment line, e.g `# my comment goes here`
	Commentf(format string, v ...interface{})

	// Errorf shows a Buildkite formatted error expands the previous group
	Errorf(format string, v ...interface{})

	// Warningf shows a buildkite boostrap warning
	Warningf(format string, v ...interface{})

	// Promptf prints a shell prompt
	Promptf(format string, v ...interface{})
}

// StderrLogger is a Logger that writes to Stdout
var StderrLogger = &WriterLogger{
	Writer: os.Stderr,
}

// DiscardLogger discards all log messages
var DiscardLogger = &WriterLogger{
	Writer: ioutil.Discard,
}

// WriterLogger provides a logger that writes to an io.Writer
type WriterLogger struct {
	Writer io.Writer
}

func (wl *WriterLogger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(wl.Writer, "%s\n", fmt.Sprintf(format, v...))
}

func (wl *WriterLogger) Headerf(format string, v ...interface{}) {
	fmt.Fprintf(wl.Writer, "~~~ %s\n", fmt.Sprintf(format, v...))
}

func (wl *WriterLogger) Commentf(format string, v ...interface{}) {
	fmt.Fprintf(wl.Writer, "\033[90m# %s\033[0m\n", fmt.Sprintf(format, v...))
}

func (wl *WriterLogger) Errorf(format string, v ...interface{}) {
	wl.Printf("\033[31m🚨 Error: %s\033[0m", fmt.Sprintf(format, v...))
	wl.Printf("^^^ +++")
}

func (wl *WriterLogger) Warningf(format string, v ...interface{}) {
	wl.Printf("\033[33m⚠️ Warning: %s\033[0m", fmt.Sprintf(format, v...))
	wl.Printf("^^^ +++")
}

func (wl *WriterLogger) Promptf(format string, v ...interface{}) {
	if runtime.GOOS == "windows" {
		fmt.Fprintf(wl.Writer, "\033[90m>\033[0m %s\n", fmt.Sprintf(format, v...))
	} else {
		fmt.Fprintf(wl.Writer, "\033[90m$\033[0m %s\n", fmt.Sprintf(format, v...))
	}
}
