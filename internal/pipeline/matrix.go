package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"golang.org/x/exp/maps"
	"gopkg.in/yaml.v3"
)

// Matrices (like many of the constructs in the pipeline yaml 🙃) can be represented in two ways:
// 1. The (more common?) multidimensional form, ie the yaml:
// steps:
//   - command: echo "Cool shape! It's {{matrix.shape}} and {{matrix.color}}"
//     matrix:
//       shape:
//       - circle
//       - square
//       - triangle
//       color:
//       - red
//       - green
//       - blue

// This matrix has two dimensions, shape and color, and in the Matrix struct is represented as:
// Matrix{
//	Contents: map[string][]any{
//		"shape": []any{
//			"circle",
//			"square",
//			"triangle",
//		},
//		"color": []any{
//			"red",
//			"green",
//			"blue",
//		},
//	},
// }

// 2. The "implicit single dimension" form, ie the yaml:
// steps:
//   - command: echo "Cool shape! It's {{matrix}}"
//     matrix:
//     - circle
//     - square
//     - triangle

// In this case, the matrix is represented as:
// Matrix{
//	Contents: map[string][]any{
//		"[[default]]": []any{
//			"circle",
//			"square",
//			"triangle",
//		},
//	},
// }

// When marshalling, we detect the use of the default and marshal it back into the implicit single dimension form

const defaultDimension = "[[default]]"

type Matrix struct {
	Contents map[string][]any
}

var _ interface {
	ordered.Unmarshaler
	json.Marshaler
	json.Unmarshaler
	yaml.Marshaler
} = (*Matrix)(nil)

type MatrixVariant struct {
	SelectedDimensions []SelectedDimension
}

// Equal returns true iff the two MatrixVariants contain the same dimension/value pairs (order is not important)
func (m MatrixVariant) Equal(other MatrixVariant) bool {
	mMap := make(map[string]any, len(m.SelectedDimensions))
	for _, sd := range m.SelectedDimensions {
		mMap[sd.Dimension] = sd.Value
	}

	oMap := make(map[string]any, len(other.SelectedDimensions))
	for _, sd := range other.SelectedDimensions {
		oMap[sd.Dimension] = sd.Value
	}

	return maps.Equal(mMap, oMap)
}

type SelectedDimension struct {
	Dimension string
	Value     any
}

func (sd SelectedDimension) InterpolationKey() string {
	if sd.Dimension == defaultDimension {
		return "{{matrix}}"
	}

	return fmt.Sprintf("{{matrix.%s}}", sd.Dimension)
}

func (m *Matrix) Variants() []MatrixVariant {
	if len(m.Contents) == 0 {
		if v, ok := m.Contents[defaultDimension]; ok {
			variants := make([]MatrixVariant, 0, len(v))
			for _, value := range v {
				variants = append(variants, MatrixVariant{SelectedDimensions: []SelectedDimension{{Dimension: defaultDimension, Value: value}}})
			}

			return variants
		}
	}

	numVariants := 1
	possibleValues := make(map[string][]SelectedDimension, len(m.Contents))

	for k, v := range m.Contents {
		possibleValues[k] = make([]SelectedDimension, 0, len(v))
		for _, value := range v {
			possibleValues[k] = append(possibleValues[k], SelectedDimension{Dimension: k, Value: value})
		}
		numVariants *= len(v)
	}

	variants := cartesianProduct(maps.Values(possibleValues)...)
	matrixVariants := make([]MatrixVariant, 0, numVariants)
	for _, variant := range variants {
		matrixVariants = append(matrixVariants, MatrixVariant{SelectedDimensions: variant})
	}

	return matrixVariants
}

func cartesianProduct[T any](slices ...[]T) [][]T {
	if len(slices) == 0 {
		return [][]T{}
	}

	// Initialize the result with the first slice
	result := [][]T{}
	for _, element := range slices[0] {
		result = append(result, []T{element})
	}

	// Iterate through the remaining slices and combine with the existing result
	for i := 1; i < len(slices); i++ {
		tempResult := [][]T{}
		for _, element := range slices[i] {
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

func (m *Matrix) UnmarshalOrdered(src any) error {
	switch src := src.(type) {
	case []any:
		m.Contents = map[string][]any{
			defaultDimension: src,
		}

	case *ordered.MapSA:
		m.Contents = make(map[string][]any, src.Len())
		err := src.Range(func(key string, value any) error {
			switch value := value.(type) {
			case []any:
				m.Contents[key] = value
			default:
				return errors.New("matrix values must be arrays")
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("unmarshalling matrix: %w", err)
		}

	default:
		return fmt.Errorf("matrix must be an array or an object, found %T", src)
	}

	return nil
}

func (m *Matrix) MarshalJSON() ([]byte, error) {
	if len(m.Contents) == 1 {
		if v, ok := m.Contents[defaultDimension]; ok {
			return json.Marshal(v)
		}
	}

	return json.Marshal(m.Contents)
}

func (m *Matrix) UnmarshalJSON(b []byte) error {
	var anyContents any
	if err := json.Unmarshal(b, &anyContents); err != nil {
		return err
	}

	switch contents := anyContents.(type) {
	case []any:
		m.Contents = map[string][]any{
			defaultDimension: contents,
		}

	case map[string]any:
		m.Contents = make(map[string][]any, len(contents))

		for k, v := range contents {
			v, ok := v.([]any)
			if !ok {
				return errors.New("matrix values must be arrays")
			}

			m.Contents[k] = v
		}

	default:
		return fmt.Errorf("matrix must be an array or an object, found %T", contents)
	}

	return nil
}

func (m *Matrix) MarshalYAML() (interface{}, error) {
	if len(m.Contents) == 1 {
		if v, ok := m.Contents[defaultDimension]; ok {
			return v, nil
		}
	}

	return m.Contents, nil
}
