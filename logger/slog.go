package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/lmittmann/tint"
	"golang.org/x/term"
)

// LevelVar is the process-wide slog level controller. CLI flag parsing
// mutates this so that --log-level / --debug take effect at runtime.
var LevelVar = new(slog.LevelVar)

// Discard is a *slog.Logger that throws away every record.
//
// Used where a logger is required by API but the caller doesn't want
// log output, e.g. when fetching secrets to avoid leaking key names.
var Discard = slog.New(slog.DiscardHandler)

// windowsColors records whether the Windows console was successfully
// switched into ANSI virtual-terminal mode at startup. On non-Windows
// platforms it is unused.
var windowsColors bool

// Format selects how the agent's diagnostic logs are formatted.
type Format int

const (
	// FormatText emits human-readable colored output via tint.
	FormatText Format = iota
	// FormatJSON emits one JSON object per record via slog.JSONHandler.
	FormatJSON
)

// ParseFormat parses the user-supplied --log-format value. The empty
// string is treated as "text".
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "", "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	default:
		return 0, fmt.Errorf("invalid log format %q (valid values: text, json)", s)
	}
}

// ParseLevel parses the user-supplied --log-level value. The historical
// "notice" level is accepted as an alias for "info"; the historical
// "fatal" level is accepted as an alias for "error".
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "notice":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "fatal":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (valid values: debug, info, warn, error)", s)
	}
}

// Config is the input to New.
type Config struct {
	// Format selects text or JSON output.
	Format Format
	// Level is the initial level. It is stored in LevelVar so that
	// later mutations to LevelVar take effect immediately.
	Level slog.Level
	// Debug enables source-location attribution and forces the level
	// to Debug regardless of Level.
	Debug bool
	// NoColor forces color off in text output. Color is also disabled
	// when stderr is not a TTY or when NO_COLOR is set in env.
	NoColor bool
}

// New constructs the agent's primary slog logger from cfg. Output is
// written to stderr.
func New(cfg Config) *slog.Logger {
	if cfg.Debug {
		LevelVar.Set(slog.LevelDebug)
	} else {
		LevelVar.Set(cfg.Level)
	}

	var h slog.Handler
	switch cfg.Format {
	case FormatJSON:
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level:     LevelVar,
			AddSource: cfg.Debug,
		})
	default: // FormatText
		h = tint.NewHandler(os.Stderr, &tint.Options{
			Level:     LevelVar,
			AddSource: cfg.Debug,
			NoColor:   cfg.NoColor || !ColorSupported(),
		})
	}

	return slog.New(h)
}

// Fatal logs at Error level and calls os.Exit(1).
func Fatal(l *slog.Logger, msg string, args ...any) {
	l.Error(msg, args...)
	os.Exit(1)
}

// FatalContext is the context-aware variant of Fatal.
func FatalContext(ctx context.Context, l *slog.Logger, msg string, args ...any) {
	l.ErrorContext(ctx, msg, args...)
	os.Exit(1)
}

// ColorSupported reports whether stderr supports ANSI colors.
//
// Color is disabled when:
//   - the NO_COLOR environment variable is set (any value, see
//     https://no-color.org); or
//   - we're on Windows and the console couldn't be put into ANSI
//     virtual terminal mode at startup; or
//   - stderr is not a terminal.
func ColorSupported() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if runtime.GOOS == "windows" && !windowsColors {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}
