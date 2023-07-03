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

// interpolateAny interpolates (almost) anything in-place. It returns the same
// type it is passed. When passed a string, it returns a new string. Anything
// it doesn't know how to interpolate is returned unaltered.
func interpolateAny(env interpolate.Env, o any) (any, error) {
	switch o := any(o).(type) {
	case selfInterpolater:
		return o, o.interpolate(env)

	case string:
		return interpolate.Interpolate(env, o)

	case []any:
		return o, interpolateSlice(env, o)

	case []string:
		return o, interpolateSlice(env, o)

	case ordered.Slice:
		return o, interpolateSlice(env, o)

	case map[string]any:
		return o, interpolateMap(env, o)

	case map[string]string:
		return o, interpolateMap(env, o)

	case *ordered.Map[string, any]:
		return o, interpolateOrderedMap(env, o)

	case *ordered.Map[string, string]:
		return o, interpolateOrderedMap(env, o)

	default:
		return o, nil
	}
}

// interpolateSlice applies interpolateAny over any type of slice.
func interpolateSlice[E any, S ~[]E](env interpolate.Env, s S) error {
	for i, e := range s {
		// It could be a string, so replace the old value with the new.
		inte, err := interpolateAny(env, e)
		if err != nil {
			return err
		}
		if inte == nil {
			// Then e was nil to begin with. No need to update it.
			// (Asserting nil interface to a type always panics.)
			continue
		}
		s[i] = inte.(E)
	}
	return nil
}

// interpolateMap applies interpolateAny over any type of map with string keys.
func interpolateMap[V any, M ~map[string]V](env interpolate.Env, m M) error {
	for k, v := range m {
		// We interpolate both keys and values.
		intk, err := interpolate.Interpolate(env, k)
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
		if intv == nil {
			// Then v was nil to begin with.
			// In case we're changing keys, we should reassign.
			// But we don't know the type, so can't assign nil.
			// Fortunately, v itself must be the right type.
			// (Asserting nil interface to a type always panics.)
			m[intk] = v
			continue
		}
		m[intk] = intv.(V)
	}
	return nil
}

// interpolateOrderedMap applies interpolateAny over any type of ordered.Map.
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

		if intk == nil {
			// Then k was nil to begin with... weird.
			return nil
		}

		if intv == nil {
			// Then v was nil to begin with. See above.
			m.Replace(k, intk.(K), v)
			return nil
		}

		// interpolateAny preserves the type, so these assertions are safe.
		m.Replace(k, intk.(K), intv.(V))
		return nil
	})
}
