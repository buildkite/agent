package pipeline

import "testing"

func TestMatrix_ValidatePermutation_Simple(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"": {
				"Apiaceae",
				"Brassicaceae",
				"Cucurbitaceae",
				"Fabaceae",
				"Lamiaceae",
				"Rosaceae",
				"Rutaceae",
				"Solanaceae",
				47,
				true,
			},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"": "Brassicaceae",
				},
				Skip: "yes",
			},
			{
				With: MatrixAdjustmentWith{
					"": "Amaryllidaceae",
				},
			},
		},
	}

	tests := []struct {
		name      string
		perm      MatrixPermutation
		wantValid bool
	}{
		{
			name:      "basic match",
			perm:      MatrixPermutation{{Value: "Solanaceae"}},
			wantValid: true,
		},
		{
			name:      "basic match (47)",
			perm:      MatrixPermutation{{Value: 47}},
			wantValid: true,
		},
		{
			name:      "basic match (true)",
			perm:      MatrixPermutation{{Value: true}},
			wantValid: true,
		},
		{
			name:      "basic mismatch",
			perm:      MatrixPermutation{{Value: "Poaceae"}},
			wantValid: false,
		},
		{
			name:      "basic mismatch (-66)",
			perm:      MatrixPermutation{{Value: -66}},
			wantValid: false,
		},
		{
			name:      "basic mismatch (false)",
			perm:      MatrixPermutation{{Value: false}},
			wantValid: false,
		},
		{
			name:      "adjustment match",
			perm:      MatrixPermutation{{Value: "Amaryllidaceae"}},
			wantValid: true,
		},
		{
			name:      "adjustment skip",
			perm:      MatrixPermutation{{Value: "Brassicaceae"}},
			wantValid: false,
		},
		{
			name:      "invalid dimension",
			perm:      MatrixPermutation{{Dimension: "family", Value: "Rosaceae"}},
			wantValid: false,
		},
		{
			name: "wrong dimension count / repeated dimension",
			perm: MatrixPermutation{
				{Dimension: "", Value: "Lamiaceae"},
				{Dimension: "", Value: "Rosaceae"},
			},
			wantValid: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if gotValid := (err == nil); gotValid != test.wantValid {
				t.Errorf("matrix.validatePermutation(%v) = %v", test.perm, err)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_Multiple(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"family":    {"Brassicaceae", "Rosaceae", "Solanaceae"},
			"plot":      {1, 2, 3, 4, 5},
			"treatment": {false, true},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"family":    "Brassicaceae",
					"plot":      3,
					"treatment": true,
				},
				Skip: "yes",
			},
			{
				With: MatrixAdjustmentWith{
					"family":    "Amaryllidaceae",
					"plot":      6,
					"treatment": true,
				},
			},
		},
	}

	tests := []struct {
		name      string
		perm      MatrixPermutation
		wantValid bool
	}{
		{
			name: "basic match",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Solanaceae"},
				{Dimension: "plot", Value: 2},
				{Dimension: "treatment", Value: false},
			},
			wantValid: true,
		},
		{
			name: "basic mismatch",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Solanaceae"},
				{Dimension: "plot", Value: 7},
				{Dimension: "treatment", Value: false},
			},
			wantValid: false,
		},
		{
			name: "adjustment match",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Amaryllidaceae"},
				{Dimension: "plot", Value: 6},
				{Dimension: "treatment", Value: true},
			},
			wantValid: true,
		},
		{
			name: "adjustment skip",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Brassicaceae"},
				{Dimension: "plot", Value: 3},
				{Dimension: "treatment", Value: true},
			},
			wantValid: false,
		},
		{
			name: "wrong dimension count",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Rosaceae"},
				{Dimension: "plot", Value: 3},
				{Dimension: "treatment", Value: false},
				{Dimension: "crimes", Value: "p-hacking"},
			},
			wantValid: false,
		},
		{
			name: "invalid dimension",
			perm: MatrixPermutation{
				{Dimension: "", Value: "Rosaceae"},
				{Dimension: "plot", Value: 3},
				{Dimension: "treatment", Value: false},
			},
			wantValid: false,
		},
		{
			name: "repeated dimension",
			perm: MatrixPermutation{
				{Dimension: "family", Value: "Lamiaceae"},
				{Dimension: "family", Value: "Rosaceae"},
				{Dimension: "plot", Value: 1},
			},
			wantValid: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if gotValid := (err == nil); gotValid != test.wantValid {
				t.Errorf("matrix.validatePermutation(%v) = %v", test.perm, err)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_Nil(t *testing.T) {
	t.Parallel()

	var matrix *Matrix // nil

	tests := []struct {
		name      string
		perm      MatrixPermutation
		wantValid bool
	}{
		{
			name:      "empty permutation",
			perm:      MatrixPermutation{},
			wantValid: true,
		},
		{
			name: "non-empty permutation",
			perm: MatrixPermutation{
				{Dimension: "Twin Peaks", Value: "cherry pie"},
			},
			wantValid: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := matrix.validatePermutation(test.perm)
			if gotValid := (err == nil); gotValid != test.wantValid {
				t.Errorf("matrix.validatePermutation(%v) = %v", test.perm, err)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_InvalidAdjustment(t *testing.T) {
	t.Parallel()

	perm := MatrixPermutation{
		{Dimension: "family", Value: "Brassicaceae"},
		{Dimension: "plot", Value: 3},
		{Dimension: "treatment", Value: true},
	}

	tests := []struct {
		name   string
		matrix *Matrix
	}{
		{
			name: "wrong dimension count",
			matrix: &Matrix{
				Setup: MatrixSetup{
					"family":    {"Brassicaceae", "Rosaceae", "Solanaceae"},
					"plot":      {1, 2, 3, 4, 5},
					"treatment": {false, true},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"family":    "Brassicaceae",
							"treatment": true,
						},
					},
				},
			},
		},
		{
			name: "wrong dimensions",
			matrix: &Matrix{
				Setup: MatrixSetup{
					"family":    {"Brassicaceae", "Rosaceae", "Solanaceae"},
					"plot":      {1, 2, 3, 4, 5},
					"treatment": {false, true},
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
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if err := test.matrix.validatePermutation(perm); err == nil {
				t.Errorf("matrix.validatePermutation(%v) = %v, want non-nil error", perm, err)
			}
		})
	}
}

func TestMatrix_ValidatePermutation_RepeatAdjustment(t *testing.T) {
	t.Parallel()

	matrix := &Matrix{
		Setup: MatrixSetup{
			"family":    {"Brassicaceae", "Rosaceae", "Solanaceae"},
			"plot":      {1, 2, 3, 4, 5},
			"treatment": {false, true},
		},
		Adjustments: MatrixAdjustments{
			{
				With: MatrixAdjustmentWith{
					"family":    "Brassicaceae",
					"plot":      3,
					"treatment": true,
				},
			},
			{ // repeated adjustment! "skip: true" takes precedence.
				With: MatrixAdjustmentWith{
					"family":    "Brassicaceae",
					"plot":      3,
					"treatment": true,
				},
				Skip: "yes",
			},
		},
	}

	perm := MatrixPermutation{
		{Dimension: "family", Value: "Brassicaceae"},
		{Dimension: "plot", Value: 3},
		{Dimension: "treatment", Value: true},
	}

	if err := matrix.validatePermutation(perm); err == nil {
		t.Errorf("matrix.validatePermutation(%v) = %v, want non-nil error", perm, err)
	}
}
