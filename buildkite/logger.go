package buildkite

import (
	"bytes"
	"fmt"
	"github.com/Sirupsen/logrus"
	"strings"
)

const (
	nocolor = 0
	red     = 31
	green   = 32
	yellow  = 33
	blue    = 34
)

var Logger = initLogger()

type LogFormatter struct {
}

type LoggerFields logrus.Fields

func (f *LogFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	b := &bytes.Buffer{}

	// Start by printing the time
	fmt.Fprintf(b, "%s ", entry.Time.Format("2006-01-02 15:04:05"))

	// Upper case the log level
	levelText := strings.ToUpper(entry.Level.String())

	// If we're running the agent from a terminal, then we should show colors.
	showColors := logrus.IsTerminal()

	// Windows doesn't support colors so just cut it off.
	if MachineIsWindows() {
		showColors = false
	}

	// Print the log level, but toggle the color
	if showColors {
		levelColor := green

		if levelText == "DEBUG" {
			levelColor = blue
		} else if levelText == "WARNING" {
			levelColor = yellow
		} else if levelText == "ERROR" || levelText == "FATAL" || levelText == "PANIC" {
			levelColor = red
		}

		fmt.Fprintf(b, "\x1b[%dm[%-5s]\x1b[0m ", levelColor, levelText)
	} else {
		fmt.Fprintf(b, "[%-5s] ", levelText)
	}

	// Now print the message
	fmt.Fprintf(b, "%s ", entry.Message)

	// Print any extra data. By default, the data map has 3
	// elements.
	if len(entry.Data) > 3 {
		keys := make([]string, 0)
		for key, value := range entry.Data {
			if _, ok := value.(string); ok {
				if value != "" {
					keys = append(keys, fmt.Sprintf("%v: %s", key, value))
				}
			} else {
				keys = append(keys, fmt.Sprintf("%v: %v", key, value))
			}
		}

		fmt.Fprintf(b, "(%s)", strings.Join(keys, " "))
	}

	b.WriteByte('\n')

	return b.Bytes(), nil
}

func initLogger() *logrus.Logger {
	// Create a new instance of the logrus logging library
	var log = logrus.New()

	// Use our custom log formatter
	log.Formatter = new(LogFormatter)

	return log
}

func InDebugMode() bool {
	return Logger.Level == logrus.DebugLevel
}

func LoggerInitDebug() {
	// Enable debugging
	Logger.Level = logrus.DebugLevel
	Logger.Debug("Debugging enabled")
}
