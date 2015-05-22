package logger

import (
	"fmt"
	"golang.org/x/crypto/ssh/terminal"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	nocolor = "0"
	red     = "31"
	green   = "1;32"
	yellow  = "33"
	blue    = "34"
	gray    = "1;30"
	cyan    = "1;36"
)

var level = INFO
var colors = true
var mutex = sync.Mutex{}

func GetLevel() Level {
	return level
}

func SetLevel(l Level) {
	level = l

	if level == DEBUG {
		Debug("Debug mode enabled")
	}
}

func SetColors(b bool) {
	colors = b
}

func ColorsEnabled() bool {
	if runtime.GOOS == "windows" {
		// Boo, no colors on Windows.
		return false
	} else {
		// Colors can only be shown if STDOUT is a terminal
		if terminal.IsTerminal(int(os.Stdout.Fd())) {
			return colors
		} else {
			return false
		}
	}
}

func OutputPipe() io.Writer {
	// All logging, all the time, goes to STDERR
	return os.Stderr
}

func Debug(format string, v ...interface{}) {
	if level == DEBUG {
		log(DEBUG, format, v...)
	}
}

func Error(format string, v ...interface{}) {
	log(ERROR, format, v...)
}

func Fatal(format string, v ...interface{}) {
	log(FATAL, format, v...)
	os.Exit(1)
}

func Notice(format string, v ...interface{}) {
	log(NOTICE, format, v...)
}

func Info(format string, v ...interface{}) {
	log(INFO, format, v...)
}

func Warn(format string, v ...interface{}) {
	log(WARN, format, v...)
}

func log(l Level, format string, v ...interface{}) {
	level := strings.ToUpper(l.String())
	message := fmt.Sprintf(format, v...)
	now := time.Now().Format("2006-01-02 15:04:05")
	line := ""

	if ColorsEnabled() {
		prefixColor := green
		messageColor := nocolor

		if l == DEBUG {
			prefixColor = gray
			messageColor = gray
		} else if l == NOTICE {
			prefixColor = cyan
		} else if l == WARN {
			prefixColor = yellow
		} else if l == ERROR {
			prefixColor = red
		} else if l == FATAL {
			prefixColor = red
			messageColor = red
		}

		line = fmt.Sprintf("\x1b[%sm%s %-6s\x1b[0m \x1b[%sm%s\x1b[0m\n", prefixColor, now, level, messageColor, message)
	} else {
		line = fmt.Sprintf("%s %-6s %s\n", now, level, message)
	}

	// Make sure we're only outputing a line one at a time
	mutex.Lock()
	fmt.Fprint(OutputPipe(), line)
	mutex.Unlock()
}
