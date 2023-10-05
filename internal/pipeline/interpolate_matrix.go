package pipeline

import (
	"fmt"
	"regexp"
)

// Match double curly bois containing any whitespace, "matrix", then maybe
// a dot and a dimension name, ending with any whitespace and closing curlies.
var matrixTokenRE = regexp.MustCompile(`\{\{\s*matrix(\.[\w-\.]+)?\s*\}\}`)

type stringTransformFunc = func(string) string

// MatrixPermutation represents a possible permutation of a matrix. If a matrix has three dimensions each with three values,
// there will be 27 permutations. Each permutation is a slice of SelectedDimensions, with Dimension values being implicitly
// unique
type MatrixPermutation []SelectedDimension

// SelectedDimension represents a single dimension/value pair in a matrix permutation.
type SelectedDimension struct {
	Dimension string `json:"dimension"`
	Value     any    `json:"value"`
}

// newMatrixInterpolator creates a reusable string transformer that applies matrix
// interpolation.
func newMatrixInterpolator(mp MatrixPermutation) stringTransformFunc {
	replacements := make(map[string]string)
	for _, sd := range mp {
		if sd.Dimension == "" {
			replacements[""] = fmt.Sprint(sd.Value)
		} else {
			replacements["."+sd.Dimension] = fmt.Sprint(sd.Value)
		}
	}

	return func(src string) string {
		return matrixTokenRE.ReplaceAllStringFunc(src, func(s string) string {
			sub := matrixTokenRE.FindStringSubmatch(s)
			return replacements[sub[1]]
		})
	}
}

func matrixInterpolateAny(target any, transform stringTransformFunc) any {
	switch target := target.(type) {
	case string:
		return transform(target)

	case []any:
		new := make([]any, len(target))
		for i, v := range target {
			new[i] = matrixInterpolateAny(v, transform)
		}

		return new

	case map[string]any:
		new := make(map[string]any, len(target))
		for k, v := range target {
			new[transform(k)] = matrixInterpolateAny(v, transform)
		}

		return new
	}

	// Return anything else unchanged
	return target
}
