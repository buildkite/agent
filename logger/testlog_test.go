package logger_test

import (
	"log/slog"
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestTest_RecordsMessages(t *testing.T) {
	t.Parallel()

	l, rec := logger.Test(t, logger.QuietTb())
	l.Info("hello")
	l.Warn("look out")
	l.Error("oh no")

	got := rec.Messages()
	want := []string{"hello", "look out", "oh no"}
	if len(got) != len(want) {
		t.Fatalf("Messages() = %v, want %v", got, want)
	}
	for i, m := range want {
		if got[i] != m {
			t.Errorf("Messages()[%d] = %q, want %q", i, got[i], m)
		}
	}
}

func TestTest_HasMessage(t *testing.T) {
	t.Parallel()

	l, rec := logger.Test(t, logger.QuietTb())
	l.Info("uploading artifact pkg.tar.gz")

	if !rec.HasMessage("artifact") {
		t.Errorf("HasMessage(\"artifact\") = false, want true")
	}
	if rec.HasMessage("missing") {
		t.Errorf("HasMessage(\"missing\") = true, want false")
	}
}

func TestTest_MessagesAtLevel(t *testing.T) {
	t.Parallel()

	l, rec := logger.Test(t, logger.QuietTb())
	l.Info("info-1")
	l.Warn("warn-1")
	l.Info("info-2")

	got := rec.MessagesAtLevel(slog.LevelInfo)
	if len(got) != 2 || got[0] != "info-1" || got[1] != "info-2" {
		t.Errorf("MessagesAtLevel(INFO) = %v, want [info-1 info-2]", got)
	}
	got = rec.MessagesAtLevel(slog.LevelWarn)
	if len(got) != 1 || got[0] != "warn-1" {
		t.Errorf("MessagesAtLevel(WARN) = %v, want [warn-1]", got)
	}
}

func TestTest_WithAttrs(t *testing.T) {
	t.Parallel()

	l, rec := logger.Test(t, logger.QuietTb())
	l.With("agent", "host-1").Info("started")

	records := rec.Records()
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	var found bool
	records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "agent" && a.Value.String() == "host-1" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Errorf("agent=host-1 attr not found in record")
	}
}

func TestTest_RespectsAllLevels(t *testing.T) {
	t.Parallel()

	// Test handler must be enabled at all levels so tests can record
	// debug output regardless of process LevelVar state.
	l, rec := logger.Test(t, logger.QuietTb())
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")

	if got := len(rec.Messages()); got != 4 {
		t.Errorf("recorded %d messages, want 4 (handler must accept all levels)", got)
	}
}
