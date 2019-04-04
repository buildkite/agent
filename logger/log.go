package logger

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

const (
	nocolor   = "0"
	red       = "31"
	green     = "38;5;48"
	yellow    = "33"
	blue      = "34"
	gray      = "38;5;251"
	lightgray = "38;5;243"
	cyan      = "1;36"
)

const (
	DateFormat = "2006-01-02 15:04:05"
)

var (
	mutex         = sync.Mutex{}
	windowsColors bool
)

type Logger interface {
	Debug(format string, v ...interface{})
	Error(format string, v ...interface{})
	Fatal(format string, v ...interface{})
	Notice(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Info(format string, v ...interface{})

	WithPrefix(prefix string) Logger
	SetLevel(level Level)
	GetLevel() Level
}

type TextLogger struct {
	Level  Level
	Colors bool
	Prefix string
	Writer io.Writer
	ExitFn func()
}

func NewTextLogger() Logger {
	return &TextLogger{
		Level:  NOTICE,
		Colors: ColorsAvailable(),
		Writer: os.Stderr,
	}
}

func ColorsAvailable() bool {
	// Color support for windows is set in init
	if runtime.GOOS == "windows" && !windowsColors {
		return false
	}

	// Colors can only be shown if STDOUT is a terminal
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		return true
	}

	return false
}

// WithPrefix returns a copy of the logger with the provided prefix
func (l *TextLogger) WithPrefix(prefix string) Logger {
	clone := *l
	clone.Prefix = prefix
	return &clone
}

// SetLevel sets the level for the logger
func (l *TextLogger) SetLevel(level Level) {
	l.Level = level
}

func (l *TextLogger) Debug(format string, v ...interface{}) {
	if l.Level == DEBUG {
		l.log(DEBUG, format, v...)
	}
}

func (l *TextLogger) Error(format string, v ...interface{}) {
	l.log(ERROR, format, v...)
}

func (l *TextLogger) Fatal(format string, v ...interface{}) {
	l.log(FATAL, format, v...)
	os.Exit(1)
}

func (l *TextLogger) Notice(format string, v ...interface{}) {
	if l.Level <= NOTICE {
		l.log(NOTICE, format, v...)
	}
}

func (l *TextLogger) Info(format string, v ...interface{}) {
	if l.Level <= INFO {
		l.log(INFO, format, v...)
	}
}

func (l *TextLogger) Warn(format string, v ...interface{}) {
	if l.Level <= WARN {
		l.log(WARN, format, v...)
	}
}

func (l *TextLogger) GetLevel() Level {
	return l.Level
}

func (l *TextLogger) log(level Level, format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	now := time.Now().Format(DateFormat)
	line := ""

	if l.Colors {
		levelColor := green
		messageColor := nocolor

		switch level {
		case DEBUG:
			levelColor = gray
			messageColor = gray
		case NOTICE:
			levelColor = cyan
		case WARN:
			levelColor = yellow
		case ERROR:
			levelColor = red
		case FATAL:
			levelColor = red
			messageColor = red
		}

		if l.Prefix != "" {
			line = fmt.Sprintf("\x1b[%sm%s %-6s\x1b[0m \x1b[%sm%s\x1b[0m \x1b[%sm%s\x1b[0m\n", levelColor, now, level, lightgray, l.Prefix, messageColor, message)
		} else {
			line = fmt.Sprintf("\x1b[%sm%s %-6s\x1b[0m \x1b[%sm%s\x1b[0m\n", levelColor, now, level, messageColor, message)
		}
	} else {
		if l.Prefix != "" {
			line = fmt.Sprintf("%s %-6s %s %s\n", now, level, l.Prefix, message)
		} else {
			line = fmt.Sprintf("%s %-6s %s\n", now, level, message)
		}
	}

	// Make sure we're only outputing a line one at a time
	mutex.Lock()
	fmt.Fprint(l.Writer, line)
	mutex.Unlock()
}

var Discard = &TextLogger{
	Writer: ioutil.Discard,
}
