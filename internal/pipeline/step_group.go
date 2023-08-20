package pipeline

import (
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

var ErrInvalidGroup = errors.New("invalid group")

// GroupStep models a group step.
//
// Standard caveats apply - see the package comment.
type GroupStep struct {
	// Group is typically a key with no value. Since it must always exist in
	// a group step, here it is.
	Group Group `yaml:"group"`

	Steps Steps `yaml:"steps"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// UnmarshalOrdered unmarshals a group step from an ordered map.
func (g *GroupStep) UnmarshalOrdered(src any) error {
	switch srcT := src.(type) {
	case *ordered.MapSA:
		group, exists := srcT.Get("group")
		if !exists {
			return fmt.Errorf("unmarshalling GroupStep: %w", ErrInvalidGroup)
		}

		switch groupT := group.(type) {
		case nil:
			// `group: null` is valid in a group step
		case string:
			g.Group = NewGroupString(groupT)
		default:
			return fmt.Errorf("unmarshalling GroupStep: %w", ErrInvalidGroup)
		}

		steps, exists := srcT.Get("steps")
		if !exists {
			return fmt.Errorf("unmarshalling GroupStep: %w", ErrInvalidGroup)
		}

		if err := g.Steps.UnmarshalOrdered(steps); err != nil {
			return fmt.Errorf("unmarshalling GroupStep: %w", err)
		}

		// Since we errored if `group` or `step` were missing, this is non-negative
		numRemainingFields := srcT.Len() - 2
		if numRemainingFields == 0 {
			return nil
		}

		// Remove these fields so they don't end up in RemainingFields
		remainingFields := make(map[string]any, numRemainingFields)
		_ = srcT.Range(func(k string, v any) error {
			if k != "group" && k != "steps" {
				remainingFields[k] = v
			}
			return nil
		})

		g.RemainingFields = remainingFields

		return nil

	default:
		fmt.Printf("srcT: %T\n", srcT)
		type wrappedGroup GroupStep
		if err := ordered.Unmarshal(src, (*wrappedGroup)(g)); err != nil {
			return fmt.Errorf("unmarshalling GroupStep: %w", err)
		}

		// Ensure Steps is never nil. Server side expects a sequence.
		if g.Steps == nil {
			g.Steps = Steps{}
		}
		return nil
	}
}

func (g *GroupStep) interpolate(env interpolate.Env) error {
	if g.Group != nil {
		if err := g.Group.interpolate(env); err != nil {
			return err
		}
	}

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
