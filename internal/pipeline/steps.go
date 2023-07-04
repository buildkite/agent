package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
	"gopkg.in/yaml.v3"
)

// Steps contains multiple steps. It is useful for unmarshaling step sequences,
// since it has custom logic for determining the correct step type.
type Steps []Step

// UnmarshalYAML unmarshals a sequence (of steps). An error wrapping ErrNoSteps
// is returned if given an empty sequence.
func (s *Steps) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d, col %d: wrong node kind %v for step sequence", n.Line, n.Column, n.Kind)
	}
	if len(n.Content) == 0 {
		return fmt.Errorf("line %d, col %d: %w", n.Line, n.Column, ErrNoSteps)
	}

	seen := make(map[*yaml.Node]bool)
	for _, c := range n.Content {
		step, err := unmarshalStep(seen, c)
		if err != nil {
			return err
		}
		*s = append(*s, step)
	}
	return nil
}

func (s Steps) interpolate(env interpolate.Env) error {
	return interpolateSlice(env, s)
}

// unmarshalStep unmarshals a step (usually a mapping node, but possibly a
// scalar string) into the right kind of Step.
func unmarshalStep(seen map[*yaml.Node]bool, n *yaml.Node) (Step, error) {
	// Prevents infinite recursion.
	seen[n] = true
	defer delete(seen, n)

	switch n.Kind {
	case yaml.AliasNode:
		return unmarshalStep(seen, n.Alias)

	case yaml.ScalarNode:
		if n.Tag != "!!str" {
			// What kind of step is represented as a non-string scalar?
			return nil, fmt.Errorf("line %d, col %d: invalid step (scalar tag = %q, value = %q)", n.Line, n.Column, n.Tag, n.Value)
		}

		// It's just "wait".
		if n.Value == "wait" {
			return WaitStep{}, nil
		}

		// ????
		return nil, fmt.Errorf("line %d, col %d: invalid step (value = %q)", n.Line, n.Column, n.Value)

	case yaml.MappingNode:
		// Decode into a temporary map. Use *yaml.Node as the value to only
		// decode the top level.
		m := ordered.NewMap[string, *yaml.Node](len(n.Content) / 2)
		if err := n.Decode(m); err != nil {
			return nil, err
		}

		var step Step
		switch {
		case m.Contains("command") || m.Contains("commands") || m.Contains("plugins"):
			// NB: Some "command" step are commandless containers that exist
			// just to run plugins!
			step = new(CommandStep)

		case m.Contains("wait") || m.Contains("waiter"):
			step = make(WaitStep)

		case m.Contains("block") || m.Contains("input") || m.Contains("manual"):
			step = make(InputStep)

		case m.Contains("trigger"):
			step = make(TriggerStep)

		case m.Contains("group"):
			step = new(GroupStep)

		default:
			return nil, fmt.Errorf("line %d, col %d: unknown step type", n.Line, n.Column)
		}

		// Decode the step (into the right step type).
		if err := n.Decode(step); err != nil {
			return nil, err
		}
		return step, nil

	default:
		return nil, fmt.Errorf("line %d, col %d: unsupported YAML node kind %x for Step", n.Line, n.Column, n.Kind)
	}
}
