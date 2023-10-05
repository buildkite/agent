package pipeline

import (
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

type PermutationSet []MatrixPermutation

func (ps PermutationSet) Has(p MatrixPermutation) bool {
	for _, pp := range ps {
		if pp.Equals(p) {
			return true
		}
	}

	return false
}

func (m *Matrix) Permute() PermutationSet {
	valuesByDimension := make(map[string][]SelectedDimension, len(m.Setup))
	for k, v := range m.Setup {
		valuesByDimension[k] = make([]SelectedDimension, 0, len(v))
		for _, value := range v {
			valuesByDimension[k] = append(valuesByDimension[k], SelectedDimension{Dimension: k, Value: value})
		}
	}

	p := cartesianProduct(maps.Values(valuesByDimension)...)
	permutations := make(PermutationSet, 0, len(p))
	for _, variant := range p {
		permutations = append(permutations, MatrixPermutation(variant))
	}

	return adjustPermutations(permutations, m.Adjustments)
}

func cartesianProduct[T any](slcs ...[]T) [][]T {
	if len(slcs) == 0 {
		return [][]T{}
	}

	// Initialize the result with the first slice
	result := [][]T{}
	for _, element := range slcs[0] {
		result = append(result, []T{element})
	}

	// Iterate through the remaining slices and combine with the existing result
	for i := 1; i < len(slcs); i++ {
		tempResult := [][]T{}
		for _, element := range slcs[i] {
			for _, existing := range result {
				// Create a copy of the existing slice and add the new element
				newSlice := make([]T, len(existing))
				copy(newSlice, existing)
				newSlice = append(newSlice, element)
				tempResult = append(tempResult, newSlice)
			}
		}
		result = tempResult
	}

	return result
}

// MatrixPermutation represents a possible permutation of a matrix. If a matrix has three dimensions each with three values,
// there will be 27 permutations. Each permutation is a slice of SelectedDimensions, with Dimension values being implicitly
// unique
type MatrixPermutation []SelectedDimension

func NewMatrixPermutation(m map[string]any) MatrixPermutation {
	mp := make(MatrixPermutation, 0, len(m))
	for k, v := range m {
		mp = append(mp, SelectedDimension{Dimension: k, Value: v})
	}

	return mp
}

func (mp MatrixPermutation) Equals(other MatrixPermutation) bool {
	if len(mp) != len(other) {
		return false
	}

	slices.SortFunc(mp, SelectedDimension.less) // Note: https://go.dev/ref/spec#Method_expressions
	slices.SortFunc(other, SelectedDimension.less)

	for i, sd := range mp {
		if sd.Dimension != other[i].Dimension || sd.Value != other[i].Value {
			return false
		}
	}

	return true
}

// SelectedDimension represents a single dimension/value pair in a matrix permutation.
type SelectedDimension struct {
	Dimension string `json:"dimension"`
	Value     any    `json:"value"`
}

func (sd SelectedDimension) less(other SelectedDimension) bool {
	if sd.Dimension < other.Dimension {
		return true
	}

	return false
}

func adjustPermutations(perms PermutationSet, adjustments MatrixAdjustments) PermutationSet {
	adjustedPerms := make(PermutationSet, 0, len(perms))
	skips := make([]MatrixPermutation, 0, len(adjustments))

	for _, adj := range adjustments {
		if adj.ShouldSkip() {
			skips = append(skips, NewMatrixPermutation(adj.With))
			continue
		}

		adjustedPerms = append(adjustedPerms, NewMatrixPermutation(adj.With))
	}

	for _, perm := range perms {
		if isSkippable(perm, skips) {
			continue
		}

		adjustedPerms = append(adjustedPerms, perm)
	}

	return adjustedPerms
}

func isSkippable(perm MatrixPermutation, skips []MatrixPermutation) bool {
	for _, skip := range skips {
		if perm.Equals(skip) {
			return true
		}
	}

	return false
}
