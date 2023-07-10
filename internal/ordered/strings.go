package ordered

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Strings is []string, but unmarshaling handles both sequences and single
// scalars.
type Strings []string

// UnmarshalYAML unmarshals n depending on its Kind as either
// - a sequence of strings (into a slice), or
// - a single string (into a one-element slice).
// For example, unmarshaling either `["foo"]` or `"foo"` should result in a
// one-element slice (`Strings{"foo"}`).
func (s *Strings) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var x string
		if err := n.Decode(&x); err != nil {
			return err
		}
		*s = append(*s, x)

	case yaml.SequenceNode:
		var xs []string
		if err := n.Decode(&xs); err != nil {
			return err
		}
		*s = append(*s, xs...)

	default:
		return fmt.Errorf("line %d, col %d: unsupported kind %x for unmarshaling Strings (want %x or %x)", n.Line, n.Column, n.Kind, yaml.ScalarNode, yaml.SequenceNode)
	}

	return nil
}
