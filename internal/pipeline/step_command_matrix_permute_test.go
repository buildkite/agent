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
		{
			name: "boss level: the agent's own build matrix",
			matrix: Matrix{
				Setup: MatrixSetup{
					"os":   MatrixScalars{"darwin", "freebsd", "linux", "openbsd", "windows"},
					"arch": MatrixScalars{"386", "amd64", "arm64"},
				},
				Adjustments: MatrixAdjustments{
					{With: MatrixAdjustmentWith{"os": "darwin", "arch": "386"}, Skip: "macOS no longer supports x86 binaries"},
					{With: MatrixAdjustmentWith{"os": "freebsd", "arch": "arm64"}, Skip: "arm64 FreeBSD is not currently supported"},
					{With: MatrixAdjustmentWith{"os": "openbsd", "arch": "arm64"}, Skip: "arm64 OpenBSD is not currently supported"},
					{With: MatrixAdjustmentWith{"os": "dragonflybsd", "arch": "amd64"}},
					{With: MatrixAdjustmentWith{"os": "linux", "arch": "arm"}},
					{With: MatrixAdjustmentWith{"os": "linux", "arch": "armhf"}},
					{With: MatrixAdjustmentWith{"os": "linux", "arch": "ppc64"}},
					{With: MatrixAdjustmentWith{"os": "linux", "arch": "ppc64le"}},
					{With: MatrixAdjustmentWith{"os": "linux", "arch": "mips64le"}},
					{With: MatrixAdjustmentWith{"os": "linux", "arch": "s390x"}},
					{With: MatrixAdjustmentWith{"os": "netbsd", "arch": "amd64"}},
				},
			},
			expected: agentBuildMatrixPermutations, // in another file for readability
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			permutations := tc.matrix.Permute()

			// if len(permutations) != len(tc.expected) {
			// 	t.Fatalf("expected %d permutations, got %d", len(tc.expected), len(permutations))
			// }

			for _, p := range tc.expected {
				if !permutations.Has(p) {
					t.Error(pretty.Sprintf("expected permutation set: \n% #v to have permutation: \n% #v \n(order of values doesn't matter)", permutations, p))
				}
			}
		})
	}
}
