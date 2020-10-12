package logger_test

import (
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestBuffer(t *testing.T) {
	l := logger.NewBuffer()
	l.Info("hello %s", "world")
	func(x logger.Logger) {
		x.Debug("foo bar")
	}(l)
	assert.Equal(t, []string{
		"[info] hello world",
		"[debug] foo bar",
	}, l.Messages)
}
