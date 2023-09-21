package pipeline

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestVariants(t *testing.T) {
	t.Parallel()

	m := &Matrix{
		Contents: map[string][]any{
			"shape": {
				"annulus",
				"heptagram",
			},
			"color": {
				"vermillion",
				"chartreuse",
			},
		},
	}

	expectedVariants := []MatrixVariant{
		{
			SelectedDimensions: []SelectedDimension{
				{Dimension: "shape", Value: "annulus"},
				{Dimension: "color", Value: "vermillion"},
			},
		},
		{
			SelectedDimensions: []SelectedDimension{
				{Dimension: "shape", Value: "annulus"},
				{Dimension: "color", Value: "chartreuse"},
			},
		},
		{
			SelectedDimensions: []SelectedDimension{
				{Dimension: "shape", Value: "heptagram"},
				{Dimension: "color", Value: "vermillion"},
			},
		},
		{
			SelectedDimensions: []SelectedDimension{
				{Dimension: "shape", Value: "heptagram"},
				{Dimension: "color", Value: "chartreuse"},
			},
		},
	}

	// Why not use cmp.Diff like in the rest of the tests?
	// For both the output of Variants() and, for a `SelectedDimensions` type, the order doesn't matter at all - the only
	// thing that matters is that the same dimensions/value pairs are present. cmp.Diff doesn't have a (nice) way to ignore
	// order, so we check that the length is the same and that all of the expected variants are present.
	actualVariants := m.Variants()
	for _, ev := range expectedVariants {
		if !sliceHasVariant(t, actualVariants, ev) {
			t.Fatalf("expected variant %v, but it was not found", ev)
		}
	}

	if len(actualVariants) != len(expectedVariants) {
		t.Fatalf("expected %d variants, got %d", len(expectedVariants), len(actualVariants))
	}
}

func sliceHasVariant(t *testing.T, s []MatrixVariant, v MatrixVariant) bool {
	t.Helper()

	for _, sv := range s {
		if sv.Equal(v) {
			return true
		}
	}
	return false
}

func TestMarshalUnmarshalYAML(t *testing.T) {
	t.Parallel()

	type q struct {
		Matrix *Matrix `yaml:"matrix"`
	}

	cases := []struct {
		name     string
		yaml     string
		expected *Matrix
	}{
		{
			name: "implicit single dimension",
			yaml: `matrix:
  - circle
  - square
  - triangle
`,
			expected: &Matrix{
				Contents: map[string][]any{
					defaultDimension: {
						"circle",
						"square",
						"triangle",
					},
				},
			},
		},
		{
			name: "implicit single dimension with empty array",
			yaml: `matrix: []
`,
			expected: &Matrix{
				Contents: map[string][]any{
					defaultDimension: {},
				},
			},
		},
		{
			name: "multi dimensional",
			yaml: `matrix:
  color:
    - red
    - green
    - blue
  shape:
    - circle
    - square
    - triangle
`,
			expected: &Matrix{
				Contents: map[string][]any{
					"shape": {
						"circle",
						"square",
						"triangle",
					},
					"color": {
						"red",
						"green",
						"blue",
					},
				},
			},
		},
		{
			name: "multi dimensional with empty array",
			yaml: `matrix:
  color: []
  shape: []
`,
			expected: &Matrix{
				Contents: map[string][]any{
					"shape": {},
					"color": {},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			node := new(yaml.Node)
			if err := yaml.NewDecoder(strings.NewReader(tc.yaml)).Decode(node); err != nil {
				t.Fatalf("decoding yaml: %v", err)
			}

			var wrapper q
			if err := ordered.Unmarshal(node, &wrapper); err != nil {
				t.Fatalf("unmarshalling ordered.MapSA: %v", err)
			}

			if diff := cmp.Diff(tc.expected, wrapper.Matrix); diff != "" {
				t.Fatalf("parsed matrix diff (-got, +want):\n%s", diff)
			}

			b := bytes.Buffer{}
			enc := yaml.NewEncoder(&b)
			enc.SetIndent(2)
			if err := enc.Encode(wrapper); err != nil {
				t.Fatalf("error marshalling yaml: %v", err)
			}

			if b.String() != tc.yaml {
				t.Fatalf("marshalling yaml: expected %q, got %q", tc.yaml, b.String())
			}
		})
	}
}

func TestMarshalUnmarshalJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		json     string
		expected *Matrix
	}{
		{
			name: "implicit single dimension",
			json: `["circle","square","triangle"]`,
			expected: &Matrix{
				Contents: map[string][]any{
					defaultDimension: {
						"circle",
						"square",
						"triangle",
					},
				},
			},
		},
		{
			name: "implicit single dimension with empty array",
			json: `[]`,
			expected: &Matrix{
				Contents: map[string][]any{
					defaultDimension: {},
				},
			},
		},
		{
			name: "multi dimensional",
			json: `{"color":["red","green","blue"],"shape":["circle","square","triangle"]}`,
			expected: &Matrix{
				Contents: map[string][]any{
					"shape": {
						"circle",
						"square",
						"triangle",
					},
					"color": {
						"red",
						"green",
						"blue",
					},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var m *Matrix
			if err := json.Unmarshal([]byte(tc.json), &m); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tc.expected, m); diff != "" {
				t.Fatalf("parsed matrix diff (-got, +want):\n%s", diff)
			}

			b, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshalling json: %v", err)
			}

			if string(b) != tc.json {
				t.Fatalf("marshalling json: expected %q, got %q", tc.json, string(b))
			}
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		matrix   *Matrix
		expected string
	}{
		{
			name: "implicit single dimension",
			matrix: &Matrix{
				Contents: map[string][]any{
					defaultDimension: {
						"circle",
						"square",
						"triangle",
					},
				},
			},
			expected: `["circle","square","triangle"]`,
		},
		{
			name: "multi dimensional",
			matrix: &Matrix{
				Contents: map[string][]any{
					"shape": {
						"circle",
						"square",
						"triangle",
					},
					"color": {
						"red",
						"green",
						"blue",
					},
				},
			},
			expected: `{"color":["red","green","blue"],"shape":["circle","square","triangle"]}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b, err := json.Marshal(tc.matrix)
			if err != nil {
				t.Fatalf("marshalling json: %v", err)
			}

			if string(b) != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, string(b))
			}
		})
	}
}
