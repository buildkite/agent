package pipeline

import (
	"encoding/json"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
	"gopkg.in/yaml.v3"
)

var (
	_ interface {
		json.Marshaler
		ordered.Unmarshaler
		yaml.Marshaler
		selfInterpolater
	} = (*Matrix)(nil)

	_ interface {
		json.Marshaler
		selfInterpolater
	} = (*MatrixAdjustment)(nil)

	_ = []interface {
		json.Marshaler
		ordered.Unmarshaler
		yaml.Marshaler
	}{
		(*MatrixSetup)(nil),
		(*MatrixAdjustmentWith)(nil),
	}

	_ interface {
		ordered.Unmarshaler
		selfInterpolater
	} = (*MatrixScalars)(nil)
)

// Matrix models the matrix specification for command steps.
type Matrix struct {
	Setup       MatrixSetup       `yaml:"setup"`
	Adjustments MatrixAdjustments `yaml:"adjustments,omitempty"`

	RemainingFields map[string]any `yaml:",inline"`
}

// UnmarshalOrdererd unmarshals from either []any or *ordered.MapSA.
func (m *Matrix) UnmarshalOrdered(o any) error {
	switch src := o.(type) {
	case []any:
		// Single anonymous dimension matrix, no adjustments.
		//
		// matrix:
		//   - apple
		//   - 47
		s := make(MatrixScalars, 0, len(src))
		if err := ordered.Unmarshal(src, &s); err != nil {
			return err
		}
		m.Setup = MatrixSetup{"": s}

	case *ordered.MapSA:
		// Single anonymous dimension, or multiple named dimensions, with or
		// without adjustments.
		// Unmarshal into this secret wrapper type to avoid infinite recursion.
		type wrappedMatrix Matrix
		if err := ordered.Unmarshal(o, (*wrappedMatrix)(m)); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported src type for Matrix: %T", o)
	}
	return nil
}

// Reports if the matrix is a single anonymous dimension matrix with no
// adjustments or any other fields. (It's a list of items.)
func (m *Matrix) isSimple() bool {
	return len(m.Setup) == 1 && len(m.Setup[""]) != 0 && len(m.Adjustments) == 0 && len(m.RemainingFields) == 0
}

// MarshalJSON is needed to use inlineFriendlyMarshalJSON, and reduces the
// representation to a single list if the matrix is simple.
func (m *Matrix) MarshalJSON() ([]byte, error) {
	if m.isSimple() {
		return json.Marshal(m.Setup[""])
	}
	return inlineFriendlyMarshalJSON(m)
}

// MarshalYAML is needed to reduce the representation to a single slice if
// the matrix is simple.
func (m *Matrix) MarshalYAML() (any, error) {
	if m.isSimple() {
		return m.Setup[""], nil
	}
	// Just in case the YAML marshaler tries to call MarshalYAML on the output,
	// wrap m in a type without a MarshalYAML method.
	type wrappedMatrix Matrix
	return (*wrappedMatrix)(m), nil
}

func (m *Matrix) interpolate(env interpolate.Env) error {
	if m == nil {
		return nil
	}
	if err := interpolateMap(env, m.Setup); err != nil {
		return err
	}
	if err := interpolateSlice(env, m.Adjustments); err != nil {
		return err
	}
	return interpolateMap(env, m.RemainingFields)
}

// MatrixSetup is the main setup of a matrix - one or more dimensions. The cross
// product of the dimensions in the setup produces the base combinations of
// matrix values.
type MatrixSetup map[string]MatrixScalars

// MarshalJSON returns either a list (if the setup is a single anonymous
// dimension) or an object (if it contains one or more (named) dimensions).
func (ms MatrixSetup) MarshalJSON() ([]byte, error) {
	// Note that MarshalYAML (below) always returns nil error.
	o, _ := ms.MarshalYAML()
	return json.Marshal(o)
}

// MarshalYAML returns either a Scalars (if the setup is a single anonymous
// dimension) or a map (if it contains one or more (named) dimensions).
func (ms MatrixSetup) MarshalYAML() (any, error) {
	if len(ms) == 1 && len(ms[""]) > 0 {
		return ms[""], nil
	}
	return map[string]MatrixScalars(ms), nil
}

// UnmarshalOrdered unmarshals from either []any or *ordered.MapSA.
func (ms *MatrixSetup) UnmarshalOrdered(o any) error {
	if *ms == nil {
		*ms = make(MatrixSetup)
	}
	switch src := o.(type) {
	case []any:
		// Single anonymous dimension, but we only get here if its under a setup
		// key. (Maybe the user wants adjustments for their single dimension.)
		//
		// matrix:
		//   setup:
		//     - apple
		//     - 47
		s := make(MatrixScalars, 0, len(src))
		if err := ordered.Unmarshal(src, &s); err != nil {
			return err
		}
		(*ms)[""] = s

	case *ordered.MapSA:
		// One or more (named) dimensions.
		// Unmarshal into the underlying type to avoid infinite recursion.
		if err := ordered.Unmarshal(src, (*map[string]MatrixScalars)(ms)); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported src type for MatrixSetup: %T", o)
	}
	return nil
}

// MatrixAdjustments is a set of adjustments.
type MatrixAdjustments []*MatrixAdjustment

// MatrixAdjustment models an adjustment - a combination of (possibly new)
// matrix values, and skip/soft fail configuration.
type MatrixAdjustment struct {
	With MatrixAdjustmentWith `yaml:"with"`
	Skip any                  `yaml:"skip,omitempty"`

	RemainingFields map[string]any `yaml:",inline"` // NB: soft_fail is in the remaining fields
}

func (ma *MatrixAdjustment) ShouldSkip() bool {
	switch s := ma.Skip.(type) {
	case bool:
		return s

	case nil:
		return false

	default:
		return true
	}
}

// MarshalJSON is needed to use inlineFriendlyMarshalJSON.
func (ma *MatrixAdjustment) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(ma)
}

func (ma *MatrixAdjustment) interpolate(env interpolate.Env) error {
	if ma == nil {
		return nil
	}
	if err := interpolateMap(env, ma.With); err != nil {
		return err
	}
	return interpolateMap(env, ma.RemainingFields)
}

// MatrixAdjustmentWith is either a map of dimension key -> dimension value,
// or a single value (for single anonymous dimension matrices).
type MatrixAdjustmentWith map[string]any

// MarshalJSON returns either a single scalar or an object.
func (maw MatrixAdjustmentWith) MarshalJSON() ([]byte, error) {
	// Note that MarshalYAML (below) always returns nil error.
	o, _ := maw.MarshalYAML()
	return json.Marshal(o)
}

// MarshalYAML returns either a single scalar or a map.
func (maw MatrixAdjustmentWith) MarshalYAML() (any, error) {
	if len(maw) == 1 && maw[""] != nil {
		return maw[""], nil
	}
	return map[string]any(maw), nil
}

// UnmarshalOrdered unmarshals from either a scalar value (string, bool, or int)
// or *ordered.MapSA.
func (maw *MatrixAdjustmentWith) UnmarshalOrdered(o any) error {
	if *maw == nil {
		*maw = make(MatrixAdjustmentWith)
	}

	switch src := o.(type) {
	case bool, int, string:
		// A single scalar.
		// (This is how you can do adjustments on a single anonymous dimension.)
		//
		// matrix:
		//   setup:
		//     - apple
		//     - 47
		//   adjustments:
		//     - with: banana
		//       soft_fail: true
		(*maw)[""] = src

	case *ordered.MapSA:
		// A map of dimension key -> dimension value. (Tuple of dimension value
		// selections.)
		return src.Range(func(k string, v any) error {
			switch vt := v.(type) {
			case bool, int, string:
				(*maw)[k] = vt

			default:
				return fmt.Errorf("unsupported value type %T in key %q for MatrixAdjustmentsWith", v, k)
			}
			return nil
		})

	default:
		return fmt.Errorf("unsupported src type for MatrixAdjustmentsWith: %T", o)
	}
	return nil
}

// MatrixScalars accept a list of matrix values (bool, int, or string).
// Only these types are accepted by the backend, and their representations are
// generally stable between encodings (YAML, JSON, canonical, etc).
type MatrixScalars []any

// UnmarshalOrdered unmarshals []any only (and enforces that each item is a
// bool, int, or string).
func (s *MatrixScalars) UnmarshalOrdered(o any) error {
	src, ok := o.([]any)
	if !ok {
		return fmt.Errorf("unsupported type for matrix values: %T", o)
	}

	for i, a := range src {
		switch a.(type) {
		case bool, int, string:
			*s = append(*s, a)

		default:
			return fmt.Errorf("unsupported item type %T at index %d; want one of bool, int, or string", a, i)
		}
	}
	return nil
}

// This is necessary because interpolateAny, which uses a type switch, matches
// []any strictly.
func (s MatrixScalars) interpolate(env interpolate.Env) error {
	return interpolateSlice(env, s)
}
