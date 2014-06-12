package buildbox

import (
  "github.com/Sirupsen/logrus"
)


func initLogger() *logrus.Logger {
  var log = logrus.New()

  log.Level = logrus.Debug
  log.Formatter = new(logrus.TextFormatter)

  return log
}

var Logger = initLogger()
