package logger

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

const (
	nocolor   = "0"
	red       = "31"
	green     = "38;5;48"
	yellow    = "33"
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

	WithFields(fields ...Field) Logger
	SetLevel(level Level)
	GetLevel() Level
}

type TextLogger struct {
	Level  Level
	Colors bool
	Writer io.Writer
	ExitFn func()
	Fields Fields
}

func NewTextLogger() Logger {
	return &TextLogger{
		Level:  NOTICE,
		Colors: ColorsAvailable(),
		Writer: os.Stderr,
		Fields: Fields{},
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

// WithFields returns a copy of the logger with the provided fields
func (l *TextLogger) WithFields(fields ...Field) Logger {
	clone := *l
	clone.Fields.Add(fields...)
	return &clone
}

// SetLevel sets the level for the logger
func (l *TextLogger) SetLevel(level Level) {
	l.Level = level
}

func (l *TextLogger) Debug(format string, v ...interface{}) {
	if l.Level == DEBUG {
		l.PrintLine(DEBUG, format, v...)
	}
}

func (l *TextLogger) Error(format string, v ...interface{}) {
	l.PrintLine(ERROR, format, v...)
}

func (l *TextLogger) Fatal(format string, v ...interface{}) {
	l.PrintLine(FATAL, format, v...)
	os.Exit(1)
}

func (l *TextLogger) Notice(format string, v ...interface{}) {
	if l.Level <= NOTICE {
		l.PrintLine(NOTICE, format, v...)
	}
}

func (l *TextLogger) Info(format string, v ...interface{}) {
	if l.Level <= INFO {
		l.PrintLine(INFO, format, v...)
	}
}

func (l *TextLogger) Warn(format string, v ...interface{}) {
	if l.Level <= WARN {
		l.PrintLine(WARN, format, v...)
	}
}

func (l *TextLogger) GetLevel() Level {
	return l.Level
}

func (l *TextLogger) PrintLine(level Level, format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	now := time.Now().Format(DateFormat)

	var prefix string
	var line string
	var fields []string

	if fields := l.Fields.Get(`agent_name`); len(fields) > 0 {
		prefix = fields[0].Value()
	}

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

		if prefix != "" {
			line = fmt.Sprintf("\x1b[%sm%s %-6s\x1b[0m \x1b[%sm%s\x1b[0m \x1b[%sm%s\x1b[0m",
				levelColor, now, level, lightgray, prefix, messageColor, message)
		} else {
			line = fmt.Sprintf("\x1b[%sm%s %-6s\x1b[0m \x1b[%sm%s\x1b[0m",
				levelColor, now, level, messageColor, message)
		}

		for _, field := range l.Fields {
			if field.Key() == `agent_name` {
				continue
			}
			fields = append(fields, fmt.Sprintf("\x1b[%sm%s=\x1b[0m\x1b[%sm%s\x1b[0m",
				lightgray, field.Key(), lightgray, field.Value()))
		}
	} else {
		if prefix != "" {
			line = fmt.Sprintf("%s %-6s %s %s", now, level, prefix, message)
		} else {
			line = fmt.Sprintf("%s %-6s %s", now, level, message)
		}

		for _, field := range l.Fields {
			if field.Key() == `agent_name` {
				continue
			}
			fields = append(fields, fmt.Sprintf("%s=%s", field.Key(), field.Value()))
		}
	}

	// Make sure we're only outputting a line one at a time
	mutex.Lock()
	fmt.Fprint(l.Writer, line)
	if len(fields) > 0 {
		fmt.Fprintf(l.Writer, " %s", strings.Join(fields, " "))
	}
	fmt.Fprint(l.Writer, "\n")
	mutex.Unlock()
}

var Discard = &TextLogger{
	Writer: ioutil.Discard,
}

type JSONLogger struct {
	Level  Level
	Writer io.Writer
	ExitFn func()
	Fields Fields
}

func NewJSONLogger() Logger {
	return &JSONLogger{
		Level:  DEBUG,
		Writer: os.Stderr,
		Fields: Fields{},
	}
}

func (l *JSONLogger) WithFields(fields ...Field) Logger {
	clone := *l
	clone.Fields.Add(fields...)
	return &clone
}

func (l *JSONLogger) SetLevel(level Level) {
	l.Level = level
}

func (l *JSONLogger) Debug(format string, v ...interface{}) {
	if l.Level == DEBUG {
		l.PrintLine(DEBUG, format, v...)
	}
}

func (l *JSONLogger) Error(format string, v ...interface{}) {
	l.PrintLine(ERROR, format, v...)
}

func (l *JSONLogger) Fatal(format string, v ...interface{}) {
	l.PrintLine(FATAL, format, v...)
	os.Exit(1)
}

func (l *JSONLogger) Notice(format string, v ...interface{}) {
	if l.Level <= NOTICE {
		l.PrintLine(NOTICE, format, v...)
	}
}

func (l *JSONLogger) Info(format string, v ...interface{}) {
	if l.Level <= INFO {
		l.PrintLine(INFO, format, v...)
	}
}

func (l *JSONLogger) Warn(format string, v ...interface{}) {
	if l.Level <= WARN {
		l.PrintLine(WARN, format, v...)
	}
}

func (l *JSONLogger) GetLevel() Level {
	return l.Level
}

func (l *JSONLogger) PrintLine(level Level, format string, v ...interface{}) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf(`"ts":%q,`, time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf(`"level":%q,`, level.String()))
	b.WriteString(fmt.Sprintf(`"msg":%q,`, fmt.Sprintf(format, v...)))

	for _, field := range l.Fields {
		b.WriteString(fmt.Sprintf(`%q:%q,`, field.Key(), field.Value()))
	}

	// Make sure we're only outputting a line one at a time
	mutex.Lock()
	fmt.Fprintf(l.Writer, "{%s}\n", strings.TrimSuffix(b.String(), ","))
	mutex.Unlock()
}
