package job

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestFormatJobError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
		{
			name: "context.Canceled",
			err:  context.Canceled,
			want: "job cancelled",
		},
		{
			name: "context.DeadlineExceeded",
			err:  context.DeadlineExceeded,
			want: "job timed out",
		},
		{
			name: "wrapped context.Canceled",
			err:  fmt.Errorf("running command: %w", context.Canceled),
			want: "job cancelled",
		},
		{
			name: "wrapped context.DeadlineExceeded",
			err:  fmt.Errorf("running command: %w", context.DeadlineExceeded),
			want: "job timed out",
		},
		{
			name: "non-context error passes through",
			err:  errors.New("kaboom"),
			want: "kaboom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatJobError(tt.err); got != tt.want {
				t.Errorf("FormatJobError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
