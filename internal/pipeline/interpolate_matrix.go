package pipeline

import (
	"fmt"
	"regexp"
)

// Match double curly bois containing any whitespace, "matrix", then maybe
// a dot and a dimension name, ending with any whitespace and closing curlies.
var matrixTokenRE = regexp.MustCompile(`\{\{\s*matrix(\.[\w-\.]+)?\s*\}\}`)

type stringTransformFunc = func(string) string

// matrixInterpolator creates a reusable string transformer that applies matrix
// interpolation.
func matrixInterpolator(selection map[string]any) stringTransformFunc {
	replacements := make(map[string]string)
	for dim, val := range selection {
		if dim == "" {
			replacements[""] = fmt.Sprint(val)
		} else {
			replacements["."+dim] = fmt.Sprint(val)
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
