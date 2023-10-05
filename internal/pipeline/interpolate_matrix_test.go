package pipeline

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMatrixInterpolater_Simple(t *testing.T) {
	t.Parallel()
	transform := newMatrixInterpolator(MatrixPermutation{{Dimension: "", Value: "llama"}})

	tests := []struct {
		name, input, want string
	}{
		{
			name:  "no matrix",
			input: "no matrix here",
			want:  "no matrix here",
		},
		{
			name:  "one matrix",
			input: "here have a {{matrix}}",
			want:  "here have a llama",
		},
		{
			name:  "one funky-spaced matrix",
			input: "this isn't poison. it's extract of... {{     matrix     }}!",
			want:  "this isn't poison. it's extract of... llama!",
		},
		{
			name:  "three matrix",
			input: "one {{matrix}}, two {{ matrix}}, three {{matrix }}, floor",
			want:  "one llama, two llama, three llama, floor",
		},
		{
			// TODO: Reconsider this behaviour. This might not be ideal.
			name:  "mismatched matrix",
			input: "this isn't poison. it's extract of... {{matrix.alpaca}}!",
			want:  "this isn't poison. it's extract of... !",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := transform(test.input)
			if got != test.want {
				t.Errorf("transform(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestMatrixInterpolater_Multiple(t *testing.T) {
	t.Parallel()
	transform := newMatrixInterpolator(MatrixPermutation{
		{Dimension: "protagonist", Value: "kuzco"},
		{Dimension: "animal", Value: "llama"},
		{Dimension: "weapon", Value: "poison"},
	})

	tests := []struct {
		name, input, want string
	}{
		{
			name:  "no matrix",
			input: "no matrix here",
			want:  "no matrix here",
		},
		{
			name:  "one matrix",
			input: "here have a {{matrix.animal}}",
			want:  "here have a llama",
		},
		{
			name:  "two funky-spaced matrix",
			input: "this isn't {{ matrix.weapon\t}}. it's extract of... {{     matrix.animal     }}!",
			want:  "this isn't poison. it's extract of... llama!",
		},
		{
			name:  "three matrix",
			input: "one {{matrix.animal}}, two {{ matrix.animal}}, three {{matrix.weapon }}, floor",
			want:  "one llama, two llama, three poison, floor",
		},
		{
			// TODO: Reconsider this behaviour. This might not be ideal.
			name:  "mismatched matrix",
			input: "this isn't {{matrix}}. it's extract of... {{matrix.alpaca}}!",
			want:  "this isn't . it's extract of... !",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := transform(test.input)
			if got != test.want {
				t.Errorf("transform(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestMatrixInterpolateAny(t *testing.T) {
	t.Parallel()

	type mountain struct {
		Name     string
		Altitude string
	}

	cases := []struct {
		name              string
		ms                MatrixPermutation
		interpolate, want any
	}{
		{
			name:        "string",
			interpolate: "this is a {{matrix}}",
			ms:          MatrixPermutation{{Dimension: "", Value: "llama"}},
			want:        "this is a llama",
		},
		{
			name: "deeply nested interpolation",
			ms: MatrixPermutation{
				{Dimension: "mountain", Value: "cotopaxi"},
				{Dimension: "country", Value: "ecuador"},
				{Dimension: "food", Value: "bolon de verde"},
				{Dimension: "animal", Value: "andean condor"},
				{Dimension: "currency", Value: "usd"},
				{Dimension: "language", Value: "spanish"},
			},
			interpolate: []any{
				"one", "{{matrix.mountain}}", 3, "{{matrix.country}}", true,
				map[string]any{
					"animal": "{{matrix.animal}}",
					"food":   "{{matrix.food}}",
				},
				[]any{"{{matrix.currency}}", "{{matrix.language}}"},
			},
			want: []any{
				"one", "cotopaxi", 3, "ecuador", true,
				map[string]any{
					"animal": "andean condor",
					"food":   "bolon de verde",
				},
				[]any{"usd", "spanish"},
			},
		},
		{
			name: "structs don't get interpolated",
			ms: MatrixPermutation{
				{Dimension: "name", Value: "cotopaxi"},
				{Dimension: "altitude", Value: "5897m"},
			},
			interpolate: mountain{Name: "{{matrix.name}}", Altitude: "{{matrix.altitude}}"},
			want:        mountain{Name: "{{matrix.name}}", Altitude: "{{matrix.altitude}}"},
		},
		{
			name: "concrete containers (eg slices, maps that don't contain any) don't get interpolated",
			ms: MatrixPermutation{
				{Dimension: "mountain", Value: "cotopaxi"},
				{Dimension: "country", Value: "ecuador"},
				{Dimension: "animal", Value: "andean condor"},
			},
			interpolate: []any{[]string{"{{matrix.mountain}}", "{{matrix.country}}"}, map[string]string{"animal": "{{matrix.animal}}"}},
			want:        []any{[]string{"{{matrix.mountain}}", "{{matrix.country}}"}, map[string]string{"animal": "{{matrix.animal}}"}},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tf := newMatrixInterpolator(tc.ms)
			got := matrixInterpolateAny(tc.interpolate, tf)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("matrixInterpolateAny(% #v, % #v) diff (-got +want):\n%s", tc.interpolate, tc.ms, diff)
			}
		})
	}
}
