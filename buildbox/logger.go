package buildbox

import (
  "github.com/Sirupsen/logrus"
  "bytes"
  "strings"
  "fmt"
  "time"
)

const (
  nocolor = 0
  red = 31
  green = 32
  yellow = 33
  blue = 34
)

var Logger = initLogger()

type LogFormatter struct {

}

type LoggerFields logrus.Fields;

func (f *LogFormatter) Format(entry *logrus.Entry) ([]byte, error) {
  b := &bytes.Buffer{}

  // Start by printing the time
  now := time.Now()
  fmt.Fprintf(b, "%s ", now.Format("2006-01-02 15:04:05"))

  // Upper case the log level
  levelText := strings.ToUpper(entry.Data["level"].(string))

  // Print the log level, but toggle the color
  if logrus.IsTerminal() {
    levelColor := blue

    if entry.Data["level"] == "debug" {
      levelColor = green
    } else if entry.Data["level"] == "warning" {
      levelColor = yellow
    } else if entry.Data["level"] == "error" || entry.Data["level"] == "fatal" || entry.Data["level"] == "panic" {
      levelColor = red
    }

    fmt.Fprintf(b, "\x1b[%dm[%-5s]\x1b[0m ", levelColor, levelText)
  } else {
    fmt.Fprintf(b, "[%-5s] ", levelText)
  }

  // Now print the message
  fmt.Fprintf(b, "%s ", entry.Data["msg"])

  // Print any extra data. By default, the data map has 3
  // elements.
  if len(entry.Data) > 3 {
    keys := make([]string, 0)
    for key, value := range entry.Data {
      if key != "time" && key != "level" && key != "msg" {
        if _, ok := value.(string); ok {
          keys = append(keys, fmt.Sprintf("%v: %s", key, value))
        } else {
          keys = append(keys, fmt.Sprintf("%v: %v", key, value))
        }
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

func LoggerInitDebug() {
  // Enable debugging
  Logger.Level = logrus.Debug
  Logger.Debug("Debugging enabled")
}
