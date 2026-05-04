package logger_test

import (
	"log/slog"
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestParseFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    logger.Format
		wantErr bool
	}{
		{"", logger.FormatText, false},
		{"text", logger.FormatText, false},
		{"TEXT", logger.FormatText, false},
		{"json", logger.FormatJSON, false},
		{"JSON", logger.FormatJSON, false},
		{"yaml", 0, true},
		{"  text  ", 0, true}, // we don't trim whitespace
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := logger.ParseFormat(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFormat(%q) error = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFormat(%q) error = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseFormat(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"DEBUG", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"notice", slog.LevelInfo, false}, // alias
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"fatal", slog.LevelError, false}, // alias
		{"trace", 0, true},
		{"", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := logger.ParseLevel(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) error = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) error = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNew_RespectsLevel(t *testing.T) {
	t.Parallel()

	// Sanity check: building a logger with WARN level should set
	// LevelVar to WARN, and Debug() messages should be disabled.
	l := logger.New(logger.Config{
		Format: logger.FormatText,
		Level:  slog.LevelWarn,
	})

	if !l.Enabled(t.Context(), slog.LevelWarn) {
		t.Errorf("logger should be enabled at WARN")
	}
	if l.Enabled(t.Context(), slog.LevelInfo) {
		t.Errorf("logger should NOT be enabled at INFO when Level=WARN")
	}

	// Mutating LevelVar should change behavior on the same logger.
	logger.LevelVar.Set(slog.LevelDebug)
	t.Cleanup(func() { logger.LevelVar.Set(slog.LevelInfo) })

	if !l.Enabled(t.Context(), slog.LevelDebug) {
		t.Errorf("logger should be enabled at DEBUG after LevelVar.Set(Debug)")
	}
}

func TestNew_DebugForcesDebugLevel(t *testing.T) {
	t.Parallel()

	l := logger.New(logger.Config{
		Format: logger.FormatText,
		Level:  slog.LevelError, // ignored when Debug=true
		Debug:  true,
	})
	t.Cleanup(func() { logger.LevelVar.Set(slog.LevelInfo) })

	if !l.Enabled(t.Context(), slog.LevelDebug) {
		t.Errorf("Debug=true should force level to DEBUG")
	}
}

func TestDiscard_DoesNotPanic(t *testing.T) {
	t.Parallel()

	logger.Discard.Info("dropped on the floor")
	logger.Discard.With("key", "value").Error("also dropped")
}
