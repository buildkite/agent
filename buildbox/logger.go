package buildbox

import (
  "github.com/Sirupsen/logrus"
)

var Logger = initLogger()

func initLogger() *logrus.Logger {
  var log = logrus.New()

  log.Formatter = new(logrus.TextFormatter)

  return log
}

func LoggerInitDebug() {
  Logger.Level = logrus.Debug

  Logger.Debug("Debugging enabled")
}
