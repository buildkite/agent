package pipeline

import (
	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

// This file contains helpers for recursively interpolating all the strings in
// pipeline objects.

// selfInterpolater describes types that can interpolate themselves in-place.
// They can call interpolate.Interpolate on strings, or
// interpolate{Slice,Map,OrderedMap,Any} on their other contents, to do this.
type selfInterpolater interface {
	interpolate(interpolate.Env) error
}

// interpolateAny interpolates (almost) anything in-place. When passed a string,
// it returns a new string. Anything it doesn't know how to interpolate is
// returned unaltered.
func interpolateAny[T any](env interpolate.Env, o T) (T, error) {
	// The box-typeswitch-unbox dance is required because the Go compiler
	// has no type switch for type parameters.
	var err error
	a := any(o)

	switch t := a.(type) {
	case selfInterpolater:
		err = t.interpolate(env)

	case *string:
		if t == nil {
			return o, nil
		}
		*t, err = interpolate.Interpolate(env, *t)
		a = t

	case string:
		a, err = interpolate.Interpolate(env, t)

	case []any:
		err = interpolateSlice(env, t)

	case []string:
		err = interpolateSlice(env, t)

	case ordered.Slice:
		err = interpolateSlice(env, t)

	case map[string]any:
		err = interpolateMap(env, t)

	case map[string]string:
		err = interpolateMap(env, t)

	case *ordered.Map[string, any]:
		err = interpolateOrderedMap(env, t)

	case *ordered.Map[string, string]:
		err = interpolateOrderedMap(env, t)

	default:
		return o, nil
	}

	// This happens if T is an interface type and o was interface-nil to begin
	// with. (You can't type assert interface-nil.)
	if a == nil {
		var zt T
		return zt, err
	}
	return a.(T), err
}

// interpolateSlice applies interpolateAny over any type of slice. Values in the
// slice are updated in-place.
func interpolateSlice[E any, S ~[]E](env interpolate.Env, s S) error {
	for i, e := range s {
		// It could be a string, so replace the old value with the new.
		inte, err := interpolateAny(env, e)
		if err != nil {
			return err
		}
		s[i] = inte
	}
	return nil
}

// interpolateMap applies interpolateAny over any type of map. The map is
// altered in-place.
func interpolateMap[K comparable, V any, M ~map[K]V](env interpolate.Env, m M) error {
	for k, v := range m {
		// We interpolate both keys and values.
		intk, err := interpolateAny(env, k)
		if err != nil {
			return err
		}

		// V could be string, so be sure to replace the old value with the new.
		intv, err := interpolateAny(env, v)
		if err != nil {
			return err
		}

		// If the key changed due to interpolation, delete the old key.
		if k != intk {
			delete(m, k)
		}
		m[intk] = intv
	}
	return nil
}

// interpolateOrderedMap applies interpolateAny over any type of ordered.Map.
// The map is altered in-place.
func interpolateOrderedMap[K comparable, V any](env interpolate.Env, m *ordered.Map[K, V]) error {
	return m.Range(func(k K, v V) error {
		// We interpolate both keys and values.
		intk, err := interpolateAny(env, k)
		if err != nil {
			return err
		}
		intv, err := interpolateAny(env, v)
		if err != nil {
			return err
		}

		m.Replace(k, intk, intv)
		return nil
	})
}
