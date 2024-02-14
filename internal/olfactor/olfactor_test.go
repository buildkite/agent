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
		smells   []string
		input    string
		expected []bool
	}{
		{
			name:     "1_smell_is_found",
			smells:   []string{"smell"},
			input:    "smell",
			expected: []bool{true},
		},
		{
			name:     "1_smell_is_not_found",
			smells:   []string{"smell"},
			input:    "nope",
			expected: []bool{false},
		},
		{
			name:     "input_is_empty",
			smells:   []string{"smell"},
			input:    "",
			expected: []bool{false},
		},
		{
			name:     "1_smell_is_empty",
			smells:   []string{""},
			input:    "a",
			expected: []bool{true},
		},
		{
			name:     "2_smells_both_found",
			smells:   []string{"smell", "smell2"},
			input:    "smell2",
			expected: []bool{true, true},
		},
		{
			name:     "2_disjoint_smells_both_found",
			smells:   []string{"smell", "hello"},
			input:    "smell2hello",
			expected: []bool{true, true},
		},
		{
			name:     "2_smells_both_not_found",
			smells:   []string{"smell", "smell2"},
			input:    "notasmel",
			expected: []bool{false, false},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, len(test.smells), len(test.expected))

			w, olfactor := olfactor.New(io.Discard, test.smells)
			_, err := w.Write([]byte(test.input))
			assert.NilError(t, err)

			for i, smell := range test.smells {
				expected := test.expected[i]
				smelt := olfactor.Smelt(smell)
				assert.Check(t, expected == smelt, "smell: %q", smell)
			}
		})
	}
}
