// Package logutil provides narrow adapters for external logging interfaces.
package logutil

import (
	"fmt"
	"log/slog"
)

// FormatLogger adapts slog to interfaces whose Debug method accepts a printf
// format and arguments rather than a preformatted message.
type FormatLogger struct {
	Logger *slog.Logger
}

func (l FormatLogger) Debug(format string, args ...any) {
	l.Logger.Debug(fmt.Sprintf(format, args...))
}
