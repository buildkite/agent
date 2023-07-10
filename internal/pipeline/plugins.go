package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"gopkg.in/yaml.v3"
)

var _ yaml.Unmarshaler = (*Plugins)(nil)

// Plugins is a sequence of plugins. It is useful for unmarshaling.
type Plugins []*Plugin

// UnmarshalYAML unmarshals Plugins from either
//   - a sequence of "one-item mappings" (normal form), or
//   - a mapping (where order is important...non-normal form).
//
// "plugins" is supposed to be a sequence of one-item maps, since order matters.
// But some people (even us) write plugins into one big mapping and rely on
// order preservation.
func (p *Plugins) UnmarshalYAML(n *yaml.Node) error {
	// Whether processing one big map, or a sequence of small maps, the central
	// part remains the same.
	// Parse each "key: value" as "name: config", then append in order.
	unmarshalMap := func(n *yaml.Node) error {
		plugin := ordered.NewMap[string, *ordered.MapSA](len(n.Content) / 2)
		if err := n.Decode(plugin); err != nil {
			return err
		}
		return plugin.Range(func(name string, cfg *ordered.MapSA) error {
			*p = append(*p, &Plugin{
				Name:   name,
				Config: cfg,
			})
			return nil
		})
	}

	switch n.Kind {
	case yaml.AliasNode:
		return p.UnmarshalYAML(n.Alias)

	case yaml.SequenceNode:
		for _, c := range n.Content {
			if err := unmarshalMap(c); err != nil {
				return err
			}
		}

	case yaml.MappingNode:
		if err := unmarshalMap(n); err != nil {
			return err
		}

	default:
		return fmt.Errorf("line %d, col %d: unsupported YAML node kind %x for Plugins", n.Line, n.Column, n.Kind)

	}
	return nil
}
