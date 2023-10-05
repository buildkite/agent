package pipeline

import (
	"testing"

	"github.com/kr/pretty"
)

func TestMatrixPermute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		matrix   Matrix
		expected []MatrixPermutation
	}{
		{
			name: "single unnamed dimension",
			matrix: Matrix{
				Setup: MatrixSetup{
					"": MatrixScalars{"apple", "banana"},
				},
			},
			expected: []MatrixPermutation{
				{{Dimension: "", Value: "apple"}},
				{{Dimension: "", Value: "banana"}},
			},
		},
		{
			name: "single named dimension",
			matrix: Matrix{
				Setup: MatrixSetup{
					"fruit": MatrixScalars{"apple", "banana"},
				},
			},
			expected: []MatrixPermutation{
				{{Dimension: "fruit", Value: "apple"}},
				{{Dimension: "fruit", Value: "banana"}},
			},
		},
		{
			name: "single dimension with addition",
			matrix: Matrix{
				Setup: MatrixSetup{
					"fruit": MatrixScalars{"apple", "banana"},
				},
				Adjustments: MatrixAdjustments{
					{With: MatrixAdjustmentWith{"fruit": "orange"}},
				},
			},
			expected: []MatrixPermutation{
				{{Dimension: "fruit", Value: "orange"}},
				{{Dimension: "fruit", Value: "apple"}},
				{{Dimension: "fruit", Value: "banana"}},
			},
		},
		{
			name: "single dimension with addition and skip",
			matrix: Matrix{
				Setup: MatrixSetup{
					"fruit": MatrixScalars{"apple", "banana"},
				},
				Adjustments: MatrixAdjustments{
					{With: MatrixAdjustmentWith{"fruit": "orange"}},
					{With: MatrixAdjustmentWith{"fruit": "banana"}, Skip: true},
				},
			},
			expected: []MatrixPermutation{
				{{Dimension: "fruit", Value: "orange"}},
				{{Dimension: "fruit", Value: "apple"}},
			},
		},
		{
			name: "multiple dimensions",
			matrix: Matrix{
				Setup: MatrixSetup{
					"number": MatrixScalars{"one", "two"},
					"colour": MatrixScalars{"red", "blue"},
					"animal": MatrixScalars{"fish"},
				},
			},
			expected: []MatrixPermutation{
				{
					{Dimension: "colour", Value: "red"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "one"},
				},
				{
					{Dimension: "colour", Value: "blue"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "one"},
				},
				{
					{Dimension: "colour", Value: "red"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "two"},
				},
				{
					{Dimension: "colour", Value: "blue"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "two"},
				},
			},
		},
		{
			name: "multiple dimensions with addition",
			matrix: Matrix{
				Setup: MatrixSetup{
					"number": MatrixScalars{"one", "two"},
					"colour": MatrixScalars{"red", "blue"},
					"animal": MatrixScalars{"fish"},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"number": "three",
							"colour": "purple",
							"animal": "capybara",
						},
					},
				},
			},
			expected: []MatrixPermutation{
				{
					{Dimension: "colour", Value: "red"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "one"},
				},
				{
					{Dimension: "colour", Value: "blue"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "one"},
				},
				{
					{Dimension: "colour", Value: "red"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "two"},
				},
				{
					{Dimension: "colour", Value: "blue"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "two"},
				},
				{
					{Dimension: "colour", Value: "purple"},
					{Dimension: "animal", Value: "capybara"},
					{Dimension: "number", Value: "three"},
				},
			},
		},
		{
			name: "multiple dimensions with addition and skip",
			matrix: Matrix{
				Setup: MatrixSetup{
					"number": MatrixScalars{"one", "two"},
					"colour": MatrixScalars{"red", "blue"},
					"animal": MatrixScalars{"fish"},
				},
				Adjustments: MatrixAdjustments{
					{
						With: MatrixAdjustmentWith{
							"number": "three",
							"colour": "purple",
							"animal": "capybara",
						},
					},
					{
						With: MatrixAdjustmentWith{
							"number": "two",
							"colour": "blue",
							"animal": "fish",
						},
						Skip: true,
					},
				},
			},
			expected: []MatrixPermutation{
				{
					{Dimension: "colour", Value: "red"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "one"},
				},
				{
					{Dimension: "colour", Value: "blue"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "one"},
				},
				{
					{Dimension: "colour", Value: "red"},
					{Dimension: "animal", Value: "fish"},
					{Dimension: "number", Value: "two"},
				},
				// {
				// 	{Dimension: "colour", Value: "blue"},
				// 	{Dimension: "animal", Value: "fish"}, // She's been skipped
				// 	{Dimension: "number", Value: "two"},
				// },
				{
					{Dimension: "colour", Value: "purple"},
					{Dimension: "animal", Value: "capybara"},
					{Dimension: "number", Value: "three"},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			permutations := tc.matrix.Permute()

			if len(permutations) != len(tc.expected) {
				t.Fatalf("expected %d permutations, got %d", len(tc.expected), len(permutations))
			}

			for _, p := range tc.expected {
				if !permutations.Has(p) {
					t.Error(pretty.Sprintf("expected permutation set: \n% #v to have permutation: \n% #v \n(order of values doesn't matter)", permutations, p))
				}
			}
		})
	}
}
