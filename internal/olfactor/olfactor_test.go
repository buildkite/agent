package olfactor_test

import (
	"io"
	"testing"

	"github.com/buildkite/agent/v3/internal/olfactor"
	"gotest.tools/v3/assert"
)

func TestOlfactor(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		smell    string
		input    string
		expected bool
	}{
		{
			name:     "smell_is_found",
			smell:    "smell",
			input:    "smell",
			expected: true,
		},
		{
			name:     "smell_is_not_found",
			smell:    "smell",
			input:    "nope",
			expected: false,
		},
		{
			name:     "input_is_empty",
			smell:    "smell",
			input:    "",
			expected: false,
		},
		{
			name:     "smell_is_empty",
			smell:    "",
			input:    "a",
			expected: true,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w, olfactor := olfactor.New(io.Discard, test.smell)
			_, err := w.Write([]byte(test.input))
			assert.NilError(t, err)
			assert.Equal(t, test.expected, olfactor.Smelt())
		})
	}
}
