package logger_test

import (
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func TestBuffer(t *testing.T) {
	l := logger.NewBuffer()
	l.Info("hello %s", "world")
	func(x logger.Logger) {
		x.Debug("foo bar")
	}(l)
	if diff := cmp.Diff(l.Messages, []string{
		"[info] hello world",
		"[debug] foo bar",
	}); diff != "" {
		t.Errorf("l.Messages diff (-got +want):\n%s", diff)
	}
}
