// Package yamltojson provides helpers for using yaml.Node, particularly a
// converter into JSON.
package yamltojson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Patterns that match the different ways boolean values can be written in YAML
// (particularly older versions).
var (
	trueRE  = regexp.MustCompile(`y|Y|yes|Yes|YES|true|True|TRUE|on|On|ON`)
	falseRE = regexp.MustCompile(`n|N|no|No|NO|false|False|FALSE|off|Off|OFF`)
)

// ErrNotFound is returned when an item is not found.
var ErrNotFound = errors.New("not found")

// Encode writes a JSON equivalent of a YAML node n to out.
//
//   - The order of the input is preserved, where possible. This includes
//     mapping keys introduced via merges.
//   - Aliases to anchors are followed, and mapping merges are merged. Merges
//     follow YAML rules for resolving values (keys in the top-level mapping are
//     not overridden by merges, and earlier merges win over later merges).
//   - If n is nil, nothing is written and no error is returned.
//   - Non-scalar keys in mapping nodes are not supported, and will error.
//   - Nulls are normalised to null. Nulls are not permitted for map keys, but
//     allowed for ordinary values.
//   - Booleans are normalised to true and false. If used as keys, quoted.
//   - Integers are normalised to decimal. If used as keys, quoted.
//   - Floats are normalised to scientific notation. If used as keys, quoted.
//     NaN and ±Inf are always quoted, since JSON cannot accept these as
//     numeric literals.
//   - Strings are escaped using encoding/json.Marshal.
//   - Infinite recursion using aliases will cause an error.
//   - Recursive mapping merges are fine, however.
func Encode(out io.Writer, n *yaml.Node) error {
	return encode(make(map[*yaml.Node]bool), out, n)
}

// encode implements Encode, with an extra set of nodes used to return an error
// if there is an alias that causes infinite recursion.
func encode(seen map[*yaml.Node]bool, out io.Writer, n *yaml.Node) error {
	// If n is nil, do nothing.
	if n == nil {
		return nil
	}

	// If n has been seen already while processing the parents of n, it's an
	// infinite recursion.
	// Simple example:
	// ---
	// a: &a  // seen is empty on encoding a
	//   b: *a   // seen contains a while encoding b
	if seen[n] {
		return fmt.Errorf("line %d, col %d: infinite recursion", n.Line, n.Column)
	}
	seen[n] = true

	// n needs to be "un-seen" when this layer of recursion is done:
	defer delete(seen, n)
	// Why? seen is a map, which is used by reference, so it will be shared
	// between calls to encode, which is recursive. And unlike a merge, the
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
	case yaml.DocumentNode:
		// There's generally only one element in a document, but in case there's
		// more, comma-separate them.
		for i, e := range n.Content {
			if i > 0 {
				if _, err := out.Write([]byte(",")); err != nil {
					return err
				}
			}
			if err := encode(seen, out, e); err != nil {
				return err
			}
		}

	case yaml.MappingNode:
		// A MappingNode's content is a flat list of [key, value, key, value...]
		// Because of merges, these have to be traversed more flexibly than with
		// a single loop, hence the need for RangeMap.
		if _, err := out.Write([]byte("{")); err != nil {
			return err
		}
		first := true
		processPair := func(k string, v *yaml.Node) error {
			// Emit a comma separator, if needed
			if !first {
				if _, err := out.Write([]byte{','}); err != nil {
					return err
				}
			}
			first = false

			// Emit "key":value
			b, err := json.Marshal(k)
			if err != nil {
				return err
			}
			if _, err := out.Write(append(b, ':')); err != nil {
				return err
			}
			return encode(seen, out, v)
		}
		if err := RangeMap(n, processPair); err != nil {
			return err
		}
		if _, err := out.Write([]byte("}")); err != nil {
			return err
		}

	case yaml.SequenceNode:
		// A SequenceNode is a list. Recursively encode each element.
		if _, err := out.Write([]byte("[")); err != nil {
			return err
		}
		for i, e := range n.Content {
			// If this isn't the first item, add a comma separator.
			if i != 0 {
				if _, err := out.Write([]byte(",")); err != nil {
					return err
				}
			}
			// Each item can be anything.
			if err := encode(seen, out, e); err != nil {
				return err
			}
		}
		if _, err := out.Write([]byte("]")); err != nil {
			return err
		}

	case yaml.ScalarNode:
		// json.Marshal knows how to render scalars if they have the right Go
		// type.
		x, err := parseScalar(n)
		if err != nil {
			return err
		}
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		if _, err := out.Write(b); err != nil {
			return err
		}

	case yaml.AliasNode:
		// Follow the alias and encode that.
		return encode(seen, out, n.Alias)

	default:
		return fmt.Errorf("line %d, col %d: unsupported node kind %x", n.Line, n.Column, n.Kind)
	}
	return nil
}

// RangeMap calls f with each key/value pair in a mapping node.
// It only supports scalar keys, and converts them to canonical string values.
// Non-scalar and non-stringable keys result in an error.
// Because mapping nodes can contain merges from other mapping nodes,
// potentially via sequence nodes and aliases, this function also accepts
// sequences and aliases (that must themselves recursively only contain
// mappings, sequences, and aliases...).
func RangeMap(n *yaml.Node, f func(key string, val *yaml.Node) error) error {
	return rangeMap(make(map[*yaml.Node]bool), n, f)
}

// rangeMap implements RangeMap. It tracks mapping nodes already merged, to
// prevent infinite merge loops and avoid unnecessarily merging the same mapping
// repeatedly.
func rangeMap(merged map[*yaml.Node]bool, n *yaml.Node, f func(key string, val *yaml.Node) error) error {
	// Go-like semantics: no entries in "nil".
	if n == nil {
		return nil
	}

	// If this node has already been merged into the top-level map being ranged,
	// we don't need to merge it again.
	if merged[n] {
		return nil
	}
	merged[n] = true

	switch n.Kind {
	case yaml.MappingNode:
		// gopkg.in/yaml.v3 parses mapping node contents as a flat list:
		// key, value, key, value...
		if len(n.Content)%2 != 0 {
			return fmt.Errorf("line %d, col %d: mapping node has odd content length %d", n.Line, n.Column, len(n.Content))
		}

		// Keys at an outer level take precedence over keys being merged:
		// "its key/value pairs is inserted into the current mapping, unless the
		// key already exists in it." https://yaml.org/type/merge.html
		// But we care about key ordering!
		// This necessitates two passes:
		// 1. Obtain the keys in this map
		// 2. Range over the map again, recursing into merges.
		// While merging, ignore keys in the outer level.
		// Merges may produce new keys to ignore in subsequent merges:
		// "Keys in mapping nodes earlier in the sequence override keys
		// specified in later mapping nodes."

		// 1. A pass to get the keys at this level.
		keys := make(map[string]bool)
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]

			// Ignore merges in this pass.
			if k.Tag == "!!merge" {
				continue
			}

			// Canonicalise the key into a string and store it.
			ck, err := canonicalMapKey(k)
			if err != nil {
				return err
			}
			keys[ck] = true
		}

		// Ignore existing keys when merging. Record new keys to ignore in
		// subsequent merges.
		skipKeys := func(k string, v *yaml.Node) error {
			if keys[k] {
				return nil
			}
			keys[k] = true
			return f(k, v)
		}

		// 2. Range over each pair, recursing into merges.
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]

			// Is this pair a merge? (`<<: *foo`)
			if k.Tag == "!!merge" {
				// Recursively range over the contents of the value, which
				// could be an alias to a mapping node, or a sequence of aliases
				// to mapping nodes, which could themselves contain merges...
				if err := rangeMap(merged, v, skipKeys); err != nil {
					return err
				}
				continue
			}

			// Canonicalise the key into a string (again).
			ck, err := canonicalMapKey(k)
			if err != nil {
				return err
			}

			// Yield the canonical key and the value.
			if err := f(ck, v); err != nil {
				return err
			}
		}

	case yaml.SequenceNode:
		// Range over each element e in the sequence.
		for _, e := range n.Content {
			if err := rangeMap(merged, e, f); err != nil {
				return err
			}
		}

	case yaml.AliasNode:
		// Follow the alias and range over that.
		if err := rangeMap(merged, n.Alias, f); err != nil {
			return err
		}

	default:
		// TODO: Use %v once yaml.Kind has a String method
		return fmt.Errorf("line %d, col %d: cannot range over node kind %x", n.Line, n.Column, n.Kind)
	}
	return nil
}

// IntNode is a convenience function for making a scalar node containing an int.
func IntNode(x int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(x)}
}

// StringNode is a convenience function for making a scalar node containing a string.
func StringNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// UpsertItem will replace a value in the given mapping node with the
// given replacement or insert it if it doesn't exist.
// The existing key must have tag !!str.
// If m is nil, it returns a new mapping node.
func UpsertItem(m *yaml.Node, key string, val *yaml.Node) (*yaml.Node, error) {
	// If it is nil, make a new one.
	if m == nil {
		return &yaml.Node{
			Kind:    yaml.MappingNode,
			Tag:     "!!map",
			Content: []*yaml.Node{StringNode(key), val},
		}, nil
	}

	// It's not nil, so at least make sure it is a mapping node.
	if m.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d, col %d: node is not a mapping node", m.Line, m.Column)
	}
	if len(m.Content)%2 != 0 {
		return nil, fmt.Errorf("line %d, col %d: mapping node contains odd number of items %d", m.Line, m.Column, len(m.Content))
	}

	// Linear scan for the key.
	found := -1
	for i := 0; i < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			continue
		}
		if k.Value == key {
			found = i + 1
		}
	}

	// Update the existing value.
	if found != -1 {
		m.Content[found] = val
		return m, nil
	}

	// Insert a new value.
	m.Content = append(m.Content, StringNode(key), val)
	return m, nil
}

// LookupItem retrieves the value associated with a key from a mapping node.
// The matching key in the map, if any, must be a scalar node with tag !!str.
func LookupItem(m *yaml.Node, key string) (*yaml.Node, error) {
	if m == nil {
		return nil, ErrNotFound
	}

	var val *yaml.Node
	success := errors.New("found it")

	err := RangeMap(m, func(k string, v *yaml.Node) error {
		if k != key {
			return nil
		}
		val = v
		return success
	})
	if err == success {
		return val, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// canonicalMapKey converts a scalar value into a string suitable for use as
// a map key. YAML expects different representations of the same value, e.g.
// 0xb and 11, to be equivalent, and therefore a duplicate key. JSON requires
// all keys to be strings.
func canonicalMapKey(n *yaml.Node) (string, error) {
	x, err := parseScalar(n)
	if err != nil {
		return "", err
	}
	if x == nil {
		// Nulls are not valid JSON keys.
		return "", fmt.Errorf("line %d, col %d: null not supported as a map key", n.Line, n.Column)
	}
	switch n.Tag {
	case "!!bool":
		// Canonicalise to true or false.
		return fmt.Sprintf("%t", x), nil
	case "!!int":
		// Canonicalise to decimal.
		return fmt.Sprintf("%d", x), nil
	case "!!float":
		// Canonicalise to scientific notation.
		// Don't handle Inf or NaN specially, as they will be quoted.
		return fmt.Sprintf("%e", x), nil
	default:
		// Assume the value is already a suitable key.
		return n.Value, nil
	}
}

// parseScalar parses the value of a node according to its tag. It chooses
// types and values that have appropriate equivalents when marshalled by
// encoding/json.Marshal.
func parseScalar(n *yaml.Node) (any, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("line %d, col %d: non-scalar node not supported here (kind = %x, want %x)", n.Line, n.Column, n.Kind, yaml.ScalarNode)
	}
	switch n.Tag {
	case "!!null":
		// Represent null as nil.
		return nil, nil

	case "!!bool":
		// Accepts all possible YAML boolean values.
		switch {
		case trueRE.MatchString(n.Value):
			return true, nil

		case falseRE.MatchString(n.Value):
			return false, nil

		default:
			return "", fmt.Errorf("line %d, col %d: %q is not a valid YAML bool value", n.Line, n.Column, n.Value)
		}

	case "!!int":
		// Base-60 notation is not supported.
		x, err := strconv.ParseInt(n.Value, 0, 64)
		if err != nil {
			return "", fmt.Errorf("line %d, col %d: %q is not a supported int value", n.Line, n.Column, n.Value)
		}
		return x, nil

	case "!!float":
		// Base-60 notation is not supported.
		// Manually parse values that ParseFloat doesn't understand.
		switch n.Value {
		case ".nan":
			return math.NaN(), nil
		case "-.inf":
			return math.Inf(-1), nil
		case ".inf", "+.inf":
			return math.Inf(1), nil
		}
		// Hope that everything else is parseable.
		x, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return "", fmt.Errorf("line %d, col %d: %q is not a supported float value", n.Line, n.Column, n.Value)
		}
		return x, nil

	default:
		// Assume strings, and any other kinds of scalar, are already
		// represented canonically as the n.Value string.
		return n.Value, nil
	}
}
