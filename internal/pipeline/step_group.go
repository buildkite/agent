package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
)

// GroupStep models a group step.
//
// Standard caveats apply - see the package comment.
type GroupStep struct {
	// Group is typically a key with no value. Since it must always exist in
	// a group step, here it is.
	Group *string `yaml:"group"`

	Steps Steps `yaml:"steps"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// UnmarshalOrdered unmarshals a group step from an ordered map.
func (g *GroupStep) UnmarshalOrdered(src any) error {
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

func (g *GroupStep) interpolate(tf stringTransformer) error {
	grp, err := interpolateAny(tf, g.Group)
	if err != nil {
		return err
	}
	g.Group = grp

	if err := g.Steps.interpolate(tf); err != nil {
		return err
	}
	return interpolateMap(tf, g.RemainingFields)
}

func (GroupStep) stepTag() {}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (g *GroupStep) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(g)
}
