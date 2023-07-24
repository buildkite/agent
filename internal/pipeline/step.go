package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

var _ SignedFielder = (*CommandStep)(nil)

// Step models a step in the pipeline. It will be a pointer to one of:
// - CommandStep
// - WaitStep
// - InputStep
// - TriggerStep
// - GroupStep
type Step interface {
	stepTag() // allow only the step types below

	// unmarshalStep is responsible for choosing the right kind of step to
	// unmarshal, but it uses the map representation to figure this out.
	// So we can assume *ordered.MapSA input for unmarshaling.
	unmarshalMap(*ordered.MapSA) error

	selfInterpolater
}

// CommandStep models a command step.
//
// Standard caveats apply - see the package comment.
type CommandStep struct {
	Command   string     `yaml:"command"`
	Plugins   Plugins    `yaml:"plugins,omitempty"`
	Signature *Signature `yaml:"signature,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (c *CommandStep) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(c)
}

// unmarshalMap unmarshals a command step from an ordered map.
func (c *CommandStep) unmarshalMap(m *ordered.MapSA) error {
	return m.Range(func(k string, v any) error {
		switch k {
		case "command", "commands":
			// command and commands are aliases for the same thing, which can be
			// either one big string or a sequence of strings.
			// So we need to act as though we are unmarshaling either string or
			// []string (but with type []any).
			switch x := v.(type) {
			case []any:
				cmds := make([]string, 0, len(x))
				for _, cx := range x {
					cmds = append(cmds, fmt.Sprint(cx))
				}
				// Normalise cmds into one single command string.
				// This makes signing easier later on - it's easier to hash one
				// string consistently than it is to pick apart multiple strings
				// in a consistent way in order to hash all of them
				// consistently.
				c.Command = strings.Join(cmds, "\n")

			case string:
				c.Command = x

			default:
				// Some weird-looking command that's not a string...
				c.Command = fmt.Sprint(x)
			}

		case "plugins":
			if err := c.Plugins.unmarshalAny(v); err != nil {
				return fmt.Errorf("unmarshaling plugins: %w", err)
			}

		case "signature":
			sig := new(Signature)
			if err := sig.unmarshalAny(v); err != nil {
				return fmt.Errorf("unmarshaling signature: %w", err)
			}
			c.Signature = sig

		default:
			// Preserve any other key.
			if c.RemainingFields == nil {
				c.RemainingFields = make(map[string]any)
			}
			c.RemainingFields[k] = v
		}

		return nil
	})
}

// SignedFields returns the default fields for signing.
func (c *CommandStep) SignedFields() map[string]string {
	return map[string]string{
		"command": c.Command,
	}
}

// ValuesForFields returns the contents of fields to sign.
func (c *CommandStep) ValuesForFields(fields []string) (map[string]string, error) {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		switch f {
		case "command":
			out["command"] = c.Command
		default:
			return nil, fmt.Errorf("unknown or unsupported field for signing %q", f)
		}
	}
	if _, ok := out["command"]; !ok {
		return nil, errors.New("command is required for signature verification")
	}
	return out, nil
}

func (c *CommandStep) interpolate(env interpolate.Env) error {
	cmd, err := interpolate.Interpolate(env, c.Command)
	if err != nil {
		return err
	}
	if err := interpolateSlice(env, c.Plugins); err != nil {
		return err
	}
	// NB: Do not interpolate Signature.
	if err := interpolateMap(env, c.RemainingFields); err != nil {
		return err
	}
	c.Command = cmd
	return nil
}

func (CommandStep) stepTag() {}

// WaitStep models a wait step.
//
// Standard caveats apply - see the package comment.
type WaitStep map[string]any

// MarshalJSON marshals a wait step as "wait" if w is empty, or the only key is
// "wait" and it has nil value. Otherwise it marshals as a standard map.
func (s WaitStep) MarshalJSON() ([]byte, error) {
	if len(s) <= 1 && s["wait"] == nil {
		return json.Marshal("wait")
	}
	return json.Marshal(map[string]any(s))
}

func (s WaitStep) interpolate(env interpolate.Env) error {
	return interpolateMap(env, s)
}

func (s WaitStep) unmarshalMap(m *ordered.MapSA) error {
	return m.Range(func(k string, v any) error {
		s[k] = v
		return nil
	})
}

func (WaitStep) stepTag() {}

// InputStep models a block or input step.
//
// Standard caveats apply - see the package comment.
type InputStep map[string]any

func (s InputStep) interpolate(env interpolate.Env) error {
	return interpolateMap(env, s)
}

func (s InputStep) unmarshalMap(m *ordered.MapSA) error {
	return m.Range(func(k string, v any) error {
		s[k] = v
		return nil
	})
}

func (InputStep) stepTag() {}

// TriggerStep models a trigger step.
//
// Standard caveats apply - see the package comment.
type TriggerStep map[string]any

func (s TriggerStep) interpolate(env interpolate.Env) error {
	return interpolateMap(env, s)
}

func (s TriggerStep) unmarshalMap(m *ordered.MapSA) error {
	return m.Range(func(k string, v any) error {
		s[k] = v
		return nil
	})
}

func (TriggerStep) stepTag() {}

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
