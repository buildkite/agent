package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
)

var _ SignedFielder = (*CommandStep)(nil)

// CommandStep models a command step.
//
// Standard caveats apply - see the package comment.
type CommandStep struct {
	Command   string     `yaml:"command"`
	Plugins   Plugins    `yaml:"plugins,omitempty"`
	Signature *Signature `yaml:"signature,omitempty"`
	Matrix    any        `yaml:"matrix,omitempty"`

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

		case "matrix":
			c.Matrix = v

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
func (c *CommandStep) SignedFields() (map[string]string, error) {
	plugins := ""
	if len(c.Plugins) > 0 {
		// TODO: Reconsider using JSON here - is it stable enough?
		pj, err := json.Marshal(c.Plugins)
		if err != nil {
			return nil, err
		}
		plugins = string(pj)
	}
	return map[string]string{
		"command": c.Command,
		"plugins": plugins,
	}, nil
}

// ValuesForFields returns the contents of fields to sign.
func (c *CommandStep) ValuesForFields(fields []string) (map[string]string, error) {
	// Make a set of required fields. As fields is processed, mark them off by
	// deleting them.
	required := map[string]struct{}{
		"command": {},
		"plugins": {},
	}

	out := make(map[string]string, len(fields))
	for _, f := range fields {
		delete(required, f)

		switch f {
		case "command":
			out["command"] = c.Command

		case "plugins":
			if len(c.Plugins) == 0 {
				out["plugins"] = ""
				break
			}
			// TODO: Reconsider using JSON here - is it stable enough?
			val, err := json.Marshal(c.Plugins)
			if err != nil {
				return nil, err
			}
			out["plugins"] = string(val)

		default:
			return nil, fmt.Errorf("unknown or unsupported field for signing %q", f)
		}
	}

	if len(required) > 0 {
		missing := make([]string, 0, len(required))
		for k := range required {
			missing = append(missing, k)
		}
		return nil, fmt.Errorf("one or more required fields are not present: %v", missing)
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

	if c.Matrix, err = interpolateAny(env, c.Matrix); err != nil {
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
