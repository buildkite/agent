package logger

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	nocolor = "0"
	red     = "31"
	green   = "32"
	yellow  = "33"
	blue    = "34"
	gray    = "1;30"
)

var level = INFO
var colors = true

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

	if colors {
		prefixColor := green
		messageColor := nocolor

		if l == DEBUG {
			prefixColor = gray
			messageColor = gray
		} else if l == WARN {
			prefixColor = yellow
		} else if l == ERROR || l == FATAL {
			prefixColor = red
		}

		line = fmt.Sprintf("\x1b[%sm%s [%-5s]\x1b[0m \x1b[%sm%s\x1b[0m\n", prefixColor, now, level, messageColor, message)
	} else {
		line = fmt.Sprintf("%s [%-5s] %s\n", now, level, message)
	}

	if l == DEBUG {
		fmt.Fprintf(os.Stderr, line)
	} else {
		fmt.Fprintf(os.Stdout, line)
	}
}
