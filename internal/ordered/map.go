// Package ordered implements an ordered map type.
package ordered

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

var _ interface {
	json.Marshaler
	json.Unmarshaler
	yaml.IsZeroer
	yaml.Marshaler
	yaml.Unmarshaler
} = (*Map[string, any])(nil)

// Map is an order-preserving map with string keys. It is intended for working
// with YAML in an order-preserving way (off-spec, strictly speaking) and JSON
// (more of the same).
type Map[K comparable, V any] struct {
	items []Tuple[K, V]
	index map[K]int
}

// MapSS is a convenience alias to reduce keyboard wear.
type MapSS = Map[string, string]

// MapSA is a convenience alias to reduce keyboard wear.
type MapSA = Map[string, any]

// NewMap returns a new empty map with a given initial capacity.
func NewMap[K comparable, V any](cap int) *Map[K, V] {
	return &Map[K, V]{
		items: make([]Tuple[K, V], 0, cap),
		index: make(map[K]int, cap),
	}
}

// MapFromItems creates an Map with some items.
func MapFromItems[K comparable, V any](ps ...Tuple[K, V]) *Map[K, V] {
	m := NewMap[K, V](len(ps))
	for _, p := range ps {
		m.Set(p.Key, p.Value)
	}
	return m
}

// Len returns the number of items in the map.
func (m *Map[K, V]) Len() int {
	if m == nil {
		return 0
	}
	return len(m.index)
}

// IsZero reports if m is nil or empty. It is used by yaml.v3 to check
// emptiness.
func (m *Map[K, V]) IsZero() bool {
	return m == nil || len(m.index) == 0
}

// Get retrieves the value associated with a key, and reports if it was found.
func (m *Map[K, V]) Get(k K) (V, bool) {
	var zv V
	if m == nil {
		return zv, false
	}
	idx, ok := m.index[k]
	if !ok {
		return zv, false
	}
	return m.items[idx].Value, true
}

// Contains reports if the map contains the key.
func (m *Map[K, V]) Contains(k K) bool {
	if m == nil {
		return false
	}
	_, has := m.index[k]
	return has
}

// Set sets the value for the given key. If the key exists, it remains in its
// existing spot, otherwise it is added to the end of the map.
func (m *Map[K, V]) Set(k K, v V) {
	// Suppose someone makes Map with new(Map). The one thing we need to not be
	// nil will be nil.
	if m.index == nil {
		m.index = make(map[K]int, 1)
	}

	// Replace existing value?
	if idx, exists := m.index[k]; exists {
		m.items[idx].Value = v
		return
	}

	// Append new item.
	m.index[k] = len(m.items)
	m.items = append(m.items, Tuple[K, V]{
		Key:   k,
		Value: v,
	})
}

// Replace replaces an old key in the same spot with a new key and value.
// If the old key doesn't exist in the map, the item is inserted at the end.
// If the new key already exists in the map (and isn't equal to the old key),
// then it is deleted.
// This provides a way to change a single key in-place (easier than deleting the
// old key and all later keys, adding the new key, then restoring the rest).
func (m *Map[K, V]) Replace(old, new K, v V) {
	// Suppose someone makes Map with new(Map). The one thing we need to not be
	// nil will be nil.
	if m.index == nil {
		m.index = make(map[K]int, 1)
	}

	// idx is where the item will go
	idx, exists := m.index[old]
	if !exists {
		// Point idx at the end of m.items and ensure there is an item there.
		idx = len(m.items)
		m.items = append(m.items, Tuple[K, V]{})
	}

	// If the key changed, there's some tidyup...
	if old != new {
		// If "new" already exists in the map, then delete it first. The intent
		// of Replace is to put the item where "old" is but under "new", so if
		// "new" already exists somewhere else, adding it where "old" is would
		// be getting out of hand (now there are two of them).
		if newidx, exists := m.index[new]; exists {
			m.items[newidx].deleted = true
		}

		// Delete "old" from the index and update "new" to point to idx
		delete(m.index, old)
		m.index[new] = idx
	}

	// Put the item into m.items at idx.
	m.items[idx] = Tuple[K, V]{
		Key:   new,
		Value: v,
	}
}

// Delete deletes a key from the map. It does nothing if the key is not in the
// map.
func (m *Map[K, V]) Delete(k K) {
	if m == nil {
		return
	}
	idx, ok := m.index[k]
	if !ok {
		return
	}
	m.items[idx].deleted = true
	delete(m.index, k)

	// If half the pairs have been deleted, perform a compaction.
	if len(m.items) >= 2*len(m.index) {
		m.compact()
	}
}

// ToMap creates a regular (un-ordered) map containing the same data.
func (m *Map[K, V]) ToMap() map[K]V {
	um := make(map[K]V, len(m.index))
	m.Range(func(k K, v V) error {
		um[k] = v
		return nil
	})
	return um
}

// Equal reports if the two maps are equal (they contain the same items in the
// same order). Keys are compared directly; values are compared using go-cmp
// (provided with Equal[string, any] and Equal[string, string] as a comparers).
func Equal[K comparable, V any](a, b *Map[K, V]) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Len() != b.Len() {
		return false
	}
	i, j := 0, 0
	for i < len(a.items) && j < len(b.items) {
		for a.items[i].deleted {
			i++
		}
		for b.items[j].deleted {
			j++
		}
		if a.items[i].Key != b.items[j].Key {
			return false
		}
		if !cmp.Equal(a.items[i].Value, b.items[j].Value, cmp.Comparer(Equal[string, string]), cmp.Comparer(Equal[string, any])) {
			return false
		}
		i++
		j++
	}
	return true
}

// EqualSS is a convenience alias to reduce keyboard wear.
var EqualSS = Equal[string, string]

// EqualSA is a convenience alias to reduce keyboard wear.
var EqualSA = Equal[string, any]

// compact re-organises the internal storage of the Map.
func (m *Map[K, V]) compact() {
	pairs := make([]Tuple[K, V], 0, len(m.index))
	for _, p := range m.items {
		if p.deleted {
			continue
		}
		m.index[p.Key] = len(pairs)
		pairs = append(pairs, Tuple[K, V]{
			Key:   p.Key,
			Value: p.Value,
		})
	}
	m.items = pairs
}

// Range ranges over the map (in order). If f returns an error, it stops ranging
// and returns that error.
func (m *Map[K, V]) Range(f func(k K, v V) error) error {
	if m.IsZero() {
		return nil
	}
	for _, p := range m.items {
		if p.deleted {
			continue
		}
		if err := f(p.Key, p.Value); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON marshals the ordered map to JSON. It preserves the map order in
// the output.
func (m *Map[K, V]) MarshalJSON() ([]byte, error) {
	// NB: writes to b don't error, but JSON encoding could error.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	b.WriteRune('{')
	first := true
	err := m.Range(func(k K, v V) error {
		if !first {
			// Separating comma.
			b.WriteRune(',')
		}
		first = false
		if err := enc.Encode(k); err != nil {
			return err
		}
		b.WriteRune(':')
		return enc.Encode(v)
	})
	if err != nil {
		return nil, err
	}
	b.WriteRune('}')
	return b.Bytes(), nil
}

// MarshalYAML returns a *yaml.Node encoding this map (in order), or an error
// if any of the items could not be encoded into a *yaml.Node.
func (m *Map[K, V]) MarshalYAML() (any, error) {
	n := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	err := m.Range(func(k K, v V) error {
		nk, nv := new(yaml.Node), new(yaml.Node)
		if err := nk.Encode(k); err != nil {
			return err
		}
		if err := nv.Encode(v); err != nil {
			return err
		}
		n.Content = append(n.Content, nk, nv)
		return nil
	})

	if err != nil {
		return nil, err
	}
	return n, nil
}

// UnmarshalJSON unmarshals to JSON. It only supports K = string.
// This is yaml.Unmarshal in a trenchcoat (YAML is a superset of JSON).
func (m *Map[K, V]) UnmarshalJSON(b []byte) error {
	return yaml.Unmarshal(b, m)
}

// UnmarshalYAML unmarshals a YAML mapping node into this map. It only supports
// K = string. Where yaml.v3 typically infers map[string]any for unmarshaling
// mappings into any, this method chooses *Map[string, any] instead.
// If V = *yaml.Node, then the value nodes are not decoded. This is useful for
// a shallow unmarshaling step.
func (m *Map[K, V]) UnmarshalYAML(n *yaml.Node) error {
	om, ok := any(m).(*Map[string, V])
	if !ok {
		var zk K
		return fmt.Errorf("cannot unmarshal into ordered.Map with key type %T (want string)", zk)
	}

	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d, col %d: wrong kind (got %x, want %x)", n.Line, n.Column, n.Kind, yaml.MappingNode)
	}

	switch tm := any(m).(type) {
	case *Map[string, any]:
		// Use decode, then steal the contents.
		sm, err := decode(make(map[*yaml.Node]bool), n)
		if err != nil {
			return err
		}
		*tm = *sm.(*Map[string, any])
		return nil

	case *Map[string, *yaml.Node]:
		// Load into the map without any value decoding.
		return rangeYAMLMap(n, func(key string, val *yaml.Node) error {
			tm.Set(key, val)
			return nil
		})

	default:
		return rangeYAMLMap(n, func(key string, val *yaml.Node) error {
			// Try decode? (maybe V is a type like []any).
			nv, err := decode(make(map[*yaml.Node]bool), val)
			if err != nil {
				return err
			}
			v, ok := nv.(V)
			if !ok {
				// Let yaml.v3 choose what to do with the specific type.
				if err := val.Decode(&v); err != nil {
					return err
				}
			}
			om.Set(key, v)
			return nil
		})
	}
}

// decode recursively unmarshals n into a generic type (any, []any, or
// *Map[string, any]) depending on the kind of n.
func decode(seen map[*yaml.Node]bool, n *yaml.Node) (any, error) {
	// nil decodes to nil.
	if n == nil {
		return nil, nil
	}

	// If n has been seen already while processing the parents of n, it's an
	// infinite recursion.
	// Simple example:
	// ---
	// a: &a  // seen is empty on encoding a
	//   b: *a   // seen contains a while encoding b
	if seen[n] {
		return nil, fmt.Errorf("line %d, col %d: infinite recursion", n.Line, n.Column)
	}
	seen[n] = true

	// n needs to be "un-seen" when this layer of recursion is done:
	defer delete(seen, n)
	// Why? seen is a map, which is used by reference, so it will be shared
	// between calls to decode, which is recursive. And unlike a merge, the
	// same alias can be validly used for different subtrees:
	// ---
	// a: &a
	//   b: c
	// d:
	//   da: *a
	//   db: *a
	// ...
	// (d contains two copies of a).
	// So *a needs to be "unseen" between encoding "da" and "db".

	switch n.Kind {
	case yaml.ScalarNode:
		// If we need to parse more kinds of scalar, e.g. !!bool NO, or base-60
		// integers, this is where we would swap out n.Decode.
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil

	case yaml.SequenceNode:
		v := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			cv, err := decode(seen, c)
			if err != nil {
				return nil, err
			}
			v = append(v, cv)
		}
		return v, nil

	case yaml.MappingNode:
		m := NewMap[string, any](len(n.Content) / 2)
		// Why not call m.UnmarshalYAML(n) ?
		// Because we can't pass `seen` through that.
		err := rangeYAMLMap(n, func(key string, val *yaml.Node) error {
			v, err := decode(seen, val)
			if err != nil {
				return err
			}
			m.Set(key, v)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return m, nil

	case yaml.AliasNode:
		// This is one of the two ways this can blow up recursively.
		// The other (map merges) is handled by rangeMap.
		return decode(seen, n.Alias)

	case yaml.DocumentNode:
		switch len(n.Content) {
		case 0:
			return nil, nil
		case 1:
			return decode(seen, n.Content[0])
		default:
			return nil, fmt.Errorf("line %d, col %d: document contains more than 1 content item (%d)", n.Line, n.Column, len(n.Content))
		}

	default:
		return nil, fmt.Errorf("line %d, col %d: unsupported kind %x", n.Line, n.Column, n.Kind)
	}
}
