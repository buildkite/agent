package pipeline

import (
	"encoding/json"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/agent/v3/internal/yamltojson"
	"gopkg.in/yaml.v3"
)

var (
	_ json.Marshaler   = (*Plugin)(nil)
	_ yaml.Marshaler   = (*Plugin)(nil)
	_ yaml.Unmarshaler = (*Plugins)(nil)
)

// Plugin models plugin configuration.
//
// Standard caveats apply - see the package comment.
type Plugin struct {
	Name string

	// Config is stored in an ordered map in case any plugins are accidentally
	// relying on ordering.
	Config *ordered.Map[string, any]
}

// MarshalJSON returns the plugin in "one-key object" form.
func (p *Plugin) MarshalJSON() ([]byte, error) {
	o, err := p.MarshalYAML()
	if err != nil {
		return nil, err
	}
	return json.Marshal(o)
}

// MarshalYAML returns the plugin in "one-item map" form.
func (p *Plugin) MarshalYAML() (any, error) {
	return map[string]*ordered.Map[string, any]{
		p.Name: p.Config,
	}, nil
}

func (p *Plugin) interpolate(pr *Parser) error {
	name, err := pr.interpolateStr(p.Name)
	if err != nil {
		return err
	}
	p.Name = name
	if _, err := pr.interpolateAny(p.Config); err != nil {
		return err
	}
	return nil
}

// Plugins is a sequence of plugins. It is useful for unmarshaling.
type Plugins []Plugin

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
		return yamltojson.RangeMap(n, func(key string, val *yaml.Node) error {
			cfg := ordered.NewMap[string, any](len(n.Content) / 2)
			if err := val.Decode(&cfg); err != nil {
				return err
			}
			*p = append(*p, Plugin{
				Name:   key,
				Config: cfg,
			})
			return nil
		})
	}

	switch n.Kind {
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

func (p Plugins) interpolate(pr *Parser) error {
	return interpolateSlice(pr, p)
}
