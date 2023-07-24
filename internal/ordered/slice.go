package ordered

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

var _ yaml.Unmarshaler = (*Slice)(nil)

// Slice is []any, but unmarshaling into it prefers *Map[string,any] over
// map[string]any.
type Slice []any

// UnmarshalYAML unmarshals sequence nodes. Any mapping nodes are unmarshaled
// as *Map[string,any].
func (s *Slice) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d, col %d: unsupported kind %x for unmarshaling Slice (want %x)", n.Line, n.Column, n.Kind, yaml.SequenceNode)
	}
	seen := make(map[*yaml.Node]bool)
	for _, c := range n.Content {
		cv, err := decodeYAML(seen, c)
		if err != nil {
			return err
		}
		*s = append(*s, cv)
	}
	return nil
}
