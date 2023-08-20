package ordered

// Tuple is used for storing values in Map.
type Tuple[K comparable, V any] struct {
	Key   K
	Value V

	deleted bool
}

// MkTuple may be used to create a Tuple[K, V] and take advantage of type
// inference.
func MkTuple[K comparable, V any](k K, v V) Tuple[K, V] {
	return Tuple[K, V]{
		Key:   k,
		Value: v,
	}
}

// TupleSS is a convenience alias to reduce keyboard wear.
type TupleSS = Tuple[string, string]

// TupleSA is a convenience alias to reduce keyboard wear.
type TupleSA = Tuple[string, any]
