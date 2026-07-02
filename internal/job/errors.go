package job

import (
	"context"
	"errors"
)

func FormatJobError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "job timed out"
	case errors.Is(err, context.Canceled):
		return "job cancelled"
	default:
		return err.Error()
	}
}
