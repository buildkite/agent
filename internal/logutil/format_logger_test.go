package logutil

import (
	"testing"

	"github.com/buildkite/agent/v4/internal/logtest"
)

func TestFormatLoggerFormatsDebugMessage(t *testing.T) {
	t.Parallel()

	log, handler := logtest.NewLogger()
	FormatLogger{Logger: log}.Debug("verified %d %s", 2, "signatures")

	if got, want := handler.Messages(), []string{"verified 2 signatures"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("handler.Messages() = %q, want %q", got, want)
	}
}
