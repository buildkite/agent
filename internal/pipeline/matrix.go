package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
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
