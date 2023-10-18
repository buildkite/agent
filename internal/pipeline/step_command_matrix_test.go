package pipeline

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMatrix_ValidatePermutation_Simple(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"": {
				"Umbellifers",
				"Brassicas",
				"Squashes",
				"Legumes",
				"Mints",
				"Rose family",
				"Citruses",
				"Nightshades",
			},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"": "Brassicas",
				},
				Skip: "yes",
			},
			{
				With: MatrixAdjustmentWith{
					"": "Alliums",
				},
			},
		},
	}

	tests := []struct {
		name string
		perm MatrixPermutation
		want error
	}{
		{
			name: "basic match",
			perm: MatrixPermutation{"": "Nightshades"},
			want: nil,
		},
		{
			name: "basic mismatch",
			perm: MatrixPermutation{"": "Grasses"},
			want: errPermutationNoMatch,
		},
		{
			name: "adjustment match",
			perm: MatrixPermutation{"": "Alliums"},
			want: nil,
		},
		{
			name: "adjustment skip",
			perm: MatrixPermutation{"": "Brassicas"},
			want: errPermutationSkipped,
		},
		{
			name: "invalid dimension",
			perm: MatrixPermutation{"family": "Rose family"},
			want: errPermutationUnknownDimension,
		},
		{
			name: "wrong dimension count",
			perm: MatrixPermutation{
				"":       "Mints",
				"family": "Rose family",
			},
			want: errPermutationLengthMismatch,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", test.perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_Multiple(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"family":    {"Brassicas", "Rose family", "Nightshades"},
			"plot":      {"1", "2", "3", "4", "5"},
			"treatment": {"false", "true"},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"family":    "Brassicas",
					"plot":      "3",
					"treatment": "true",
				},
				Skip: "yes",
			},
			{
				With: MatrixAdjustmentWith{
					"family":    "Alliums",
					"plot":      "6",
					"treatment": "true",
				},
			},
		},
	}

	tests := []struct {
		name string
		perm MatrixPermutation
		want error
	}{
		{
			name: "basic match",
			perm: MatrixPermutation{
				"family":    "Nightshades",
				"plot":      "2",
				"treatment": "false",
			},
			want: nil,
		},
		{
			name: "basic mismatch",
			perm: MatrixPermutation{
				"family":    "Nightshades",
				"plot":      "7",
				"treatment": "false",
			},
			want: errPermutationNoMatch,
		},
		{
			name: "adjustment match",
			perm: MatrixPermutation{
				"family":    "Alliums",
				"plot":      "6",
				"treatment": "true",
			},
			want: nil,
		},
		{
			name: "adjustment skip",
			perm: MatrixPermutation{
				"family":    "Brassicas",
				"plot":      "3",
				"treatment": "true",
			},
			want: errPermutationSkipped,
		},
		{
			name: "wrong dimension count",
			perm: MatrixPermutation{
				"family":    "Rose family",
				"plot":      "3",
				"treatment": "false",
				"crimes":    "p-hacking",
			},
			want: errPermutationLengthMismatch,
		},
		{
			name: "invalid dimension",
			perm: MatrixPermutation{
				"":          "Rose family",
				"plot":      "3",
				"treatment": "false",
			},
			want: errPermutationUnknownDimension,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", test.perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_Nil(t *testing.T) {
	t.Parallel()

	var matrix *Matrix // nil

	tests := []struct {
		name string
		perm MatrixPermutation
		want error
	}{
		{
			name: "empty permutation",
			perm: MatrixPermutation{},
			want: nil,
		},
		{
			name: "non-empty permutation",
			perm: MatrixPermutation{
				"Twin Peaks": "cherry pie",
			},
			want: errNilMatrix,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", test.perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_InvalidAdjustment(t *testing.T) {
	t.Parallel()

	perm := MatrixPermutation{
		"family":    "Brassicas",
		"plot":      "3",
		"treatment": "true",
	}

	tests := []struct {
		name   string
		matrix *Matrix
		want   error
	}{
		{
			name: "wrong dimension count",
			matrix: &Matrix{
				Setup: MatrixSetup{
					"family":    {"Brassicas", "Rose family", "Nightshades"},
					"plot":      {"1", "2", "3", "4", "5"},
					"treatment": {"false", "true"},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"family":    "Brassicas",
							"treatment": "true",
						},
					},
				},
			},
			want: errAdjustmentLengthMismatch,
		},
		{
			name: "wrong dimensions",
			matrix: &Matrix{
				Setup: MatrixSetup{
					"family":    {"Brassicas", "Rose family", "Nightshades"},
					"plot":      {"1", "2", "3", "4", "5"},
					"treatment": {"false", "true"},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"suspect": "Col. Mustard",
							"room":    "Conservatory",
							"weapon":  "Spanner",
						},
					},
				},
			},
			want: errAdjustmentUnknownDimension,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := test.matrix.validatePermutation(perm)
			if !errors.Is(err, test.want) {
				t.Errorf("matrix.validatePermutation(%v) = %v, want %v", perm, err, test.want)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_RepeatAdjustment(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"family":    {"Brassicas", "Rose family", "Nightshades"},
			"plot":      {"1", "2", "3", "4", "5"},
			"treatment": {"false", "true"},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"family":    "Brassicas",
					"plot":      "3",
					"treatment": "true",
				},
			},
			{ // repeated adjustment! "skip: true" takes precedence.
				With: MatrixAdjustmentWith{
					"family":    "Brassicas",
					"plot":      "3",
					"treatment": "true",
				},
				Skip: "yes",
			},
		},
	}

	perm := MatrixPermutation{
		"family":    "Brassicas",
		"plot":      "3",
		"treatment": "true",
	}

	err := matrix.validatePermutation(perm)
	if !errors.Is(err, errPermutationSkipped) {
		t.Errorf("matrix.validatePermutation(%v) = %v, want %v", perm, err, errPermutationSkipped)
	}
}

func TestMatrixPermutation_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	var got MatrixPermutation
	const input = `{
		"family": "Brassicas",
		"plot": "3",
		"treatment": "true"
	}`

	if err := json.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q, got) = %v", input, err)
	}

	want := MatrixPermutation{
		"family":    "Brassicas",
		"plot":      "3",
		"treatment": "true",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("unmarshalled MatrixPermutation diff (-got +want):\n%s", diff)
	}
}
