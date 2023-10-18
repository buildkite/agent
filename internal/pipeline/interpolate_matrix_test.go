package pipeline

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMatrixInterpolater_Simple(t *testing.T) {
	t.Parallel()
	transform := newMatrixInterpolator(MatrixPermutation{"": "llama"})

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
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := transform.Transform(test.input)
			if err != nil {
				t.Errorf("transform.Transform(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("transform.Transform(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestMatrixInterpolater_Multiple(t *testing.T) {
	t.Parallel()
	transform := newMatrixInterpolator(MatrixPermutation{
		"protagonist": "kuzco",
		"animal":      "llama",
		"weapon":      "poison",
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
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := transform.Transform(test.input)
			if err != nil {
				t.Errorf("transform.Transform(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("transform(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestMatrixInterpolator_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, input string
		transform   matrixInterpolator
	}{
		{
			name:      "mismatched named dimensions",
			input:     "this isn't poison. it's extract of... {{matrix.alpaca}}!",
			transform: newMatrixInterpolator(MatrixPermutation{"animal": "llama"}),
		},
		{
			name:      "interpolate anonymous dimension into named token",
			input:     "this isn't {{matrix.weapon}}. it's extract of... llama!",
			transform: newMatrixInterpolator(MatrixPermutation{"": "poison"}),
		},
		{
			name:      "interpolate named dimensions into anonymous token",
			input:     "this isn't {{matrix}}. it's extract of... llama!",
			transform: newMatrixInterpolator(MatrixPermutation{"weapon": "poison"}),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.transform.Transform(test.input); err == nil {
				t.Errorf("transform.Transform(%q) error = %v, want non-nil", test.input, err)
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
			ms:          MatrixPermutation{"": "llama"},
			want:        "this is a llama",
		},
		{
			name: "deeply nested interpolation",
			ms: MatrixPermutation{
				"mountain": "cotopaxi",
				"country":  "ecuador",
				"food":     "bolon de verde",
				"animal":   "andean condor",
				"currency": "usd",
				"language": "spanish",
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
				"name":     "cotopaxi",
				"altitude": "5897m",
			},
			interpolate: mountain{Name: "{{matrix.name}}", Altitude: "{{matrix.altitude}}"},
			want:        mountain{Name: "{{matrix.name}}", Altitude: "{{matrix.altitude}}"},
		},
		{
			name: "concrete containers get interpolated",
			ms: MatrixPermutation{
				"mountain": "cotopaxi",
				"country":  "ecuador",
				"animal":   "andean condor",
			},
			interpolate: []any{[]string{"{{matrix.mountain}}", "{{matrix.country}}"}, map[string]string{"animal": "{{matrix.animal}}"}},
			want:        []any{[]string{"cotopaxi", "ecuador"}, map[string]string{"animal": "andean condor"}},
		},
		{
			name: "matrix doesn't interpolate itself",
			ms: MatrixPermutation{
				"mountain": "cotopaxi",
				"country":  "ecuador",
				"animal":   "andean condor",
			},
			interpolate: &Matrix{
				Setup: MatrixSetup{
					"fruit": []string{"banana", "{{matrix.mountain}}"},
					"shape": []string{"{{matrix.country}}", "42"},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"fruit": "{{matrix.animal}}",
							"shape": "triangle",
						},
					},
				},
			},
			want: &Matrix{
				Setup: MatrixSetup{
					"fruit": []string{"banana", "{{matrix.mountain}}"},
					"shape": []string{"{{matrix.country}}", "42"},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"fruit": "{{matrix.animal}}",
							"shape": "triangle",
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tf := newMatrixInterpolator(tc.ms)
			got, err := interpolateAny(tf, tc.interpolate)
			if err != nil {
				t.Errorf("interpolateAny(% #v, % #v) error = %v", tf, tc.interpolate, err)
			}
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("interpolateAny(% #v, % #v) diff (-got +want):\n%s", tf, tc.interpolate, diff)
			}
		})
	}
}
