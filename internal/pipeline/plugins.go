package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
)

// Plugins is a sequence of plugins. It is useful for unmarshaling.
type Plugins []*Plugin

// unmarshalAny unmarshals Plugins from either
//   - []any - originally a sequence of "one-item mappings" (normal form), or
//   - *ordered.MapSA - a mapping (where order is important...non-normal form).
//
// "plugins" is supposed to be a sequence of one-item maps, since order matters.
// But some people (even us) write plugins into one big mapping and rely on
// order preservation.
func (p *Plugins) unmarshalAny(o any) error {
	// Whether processing one big map, or a sequence of small maps, the central
	// part remains the same.
	// Parse each "key: value" as "name: config", then append in order.
	unmarshalMap := func(m *ordered.MapSA) error {
		return m.Range(func(k string, v any) error {
			*p = append(*p, &Plugin{
				Name:   k,
				Config: v,
			})
			return nil
		})
	}

	switch o := o.(type) {
	case []any:
		for _, c := range o {
			m, ok := c.(*ordered.MapSA)
			if !ok {
				return fmt.Errorf("unmarshaling plugins: plugin type %T, want *ordered.Map[string, any]", c)
			}
			if err := unmarshalMap(m); err != nil {
				return err
			}
		}

	case *ordered.MapSA:
		if err := unmarshalMap(o); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unmarshaling plugins: got %T, want []any or *ordered.Map[string, any]", o)

	}
	return nil
}
