package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

// GroupStep models a group step.
//
// Standard caveats apply - see the package comment.
type GroupStep struct {
	// Group is typically a key with no value. Since it must always exist in
	// a group step, here it is.
	Group any `yaml:"group"`

	Steps Steps `yaml:"steps"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// unmarshalMap unmarshals a group step from an ordered map.
func (g *GroupStep) unmarshalMap(m *ordered.MapSA) error {
	err := m.Range(func(k string, v any) error {
		switch k {
		case "group":
			g.Group = v

		case "steps":
			if err := g.Steps.unmarshalAny(v); err != nil {
				return fmt.Errorf("unmarshaling steps: %v", err)
			}

		default:
			// Preserve any other key.
			if g.RemainingFields == nil {
				g.RemainingFields = make(map[string]any)
			}
			g.RemainingFields[k] = v
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Ensure Steps is never nil. Server side expects a sequence.
	if g.Steps == nil {
		g.Steps = Steps{}
	}
	return nil
}

func (g *GroupStep) interpolate(env interpolate.Env) error {
	if err := g.Steps.interpolate(env); err != nil {
		return err
	}
	return interpolateMap(env, g.RemainingFields)
}

func (GroupStep) stepTag() {}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (g *GroupStep) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(g)
}
