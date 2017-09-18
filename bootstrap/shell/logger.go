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

// StderrLogger is a Logger that writes to Stderr
var StderrLogger = &WriterLogger{
	Writer: os.Stderr,
	Ansi:   true,
}

// DiscardLogger discards all log messages
var DiscardLogger = &WriterLogger{
	Writer: ioutil.Discard,
}

// WriterLogger provides a logger that writes to an io.Writer
type WriterLogger struct {
	Writer io.Writer
	Ansi   bool
}

func (wl *WriterLogger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(wl.Writer, "%s\n", fmt.Sprintf(format, v...))
}

func (wl *WriterLogger) Headerf(format string, v ...interface{}) {
	fmt.Fprintf(wl.Writer, "~~~ %s\n", fmt.Sprintf(format, v...))
}

func (wl *WriterLogger) Commentf(format string, v ...interface{}) {
	if wl.Ansi {
		wl.Printf(ansiColor("# %s", "90"), fmt.Sprintf(format, v...))
	} else {
		wl.Printf("# %s", fmt.Sprintf(format, v...))
	}
}

func (wl *WriterLogger) Errorf(format string, v ...interface{}) {
	if wl.Ansi {
		wl.Printf(ansiColor("🚨 Error: %s", "31"), fmt.Sprintf(format, v...))
	} else {
		wl.Printf("🚨 Error: %s", fmt.Sprintf(format, v...))
	}
	wl.Printf("^^^ +++")
}

func (wl *WriterLogger) Warningf(format string, v ...interface{}) {
	if wl.Ansi {
		wl.Printf(ansiColor("⚠️ Warning: %s", "33"), fmt.Sprintf(format, v...))
	} else {
		wl.Printf("⚠️ Warning: %s", fmt.Sprintf(format, v...))
	}
	wl.Printf("^^^ +++")
}

func (wl *WriterLogger) Promptf(format string, v ...interface{}) {
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	if wl.Ansi {
		wl.Printf(ansiColor(prompt, "90")+" %s", fmt.Sprintf(format, v...))
	} else {
		wl.Printf(prompt+" %s", fmt.Sprintf(format, v...))
	}
}

func ansiColor(s, attributes string) string {
	return fmt.Sprintf("\033[%sm%s\033[0m", attributes, s)
}
