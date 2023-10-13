package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/ordered"
	"gopkg.in/yaml.v3"
)

var _ interface {
	json.Marshaler
	json.Unmarshaler
	ordered.Unmarshaler
	SignedFielder
} = (*CommandStep)(nil)

// CommandStep models a command step.
//
// Standard caveats apply - see the package comment.
type CommandStep struct {
	Command   string            `yaml:"command"`
	Plugins   Plugins           `yaml:"plugins,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Signature *Signature        `yaml:"signature,omitempty"`
	Matrix    *Matrix           `yaml:"matrix,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (c *CommandStep) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(c)
}

// UnmarshalJSON is used when unmarshalling an individual step directly, e.g.
// from the Agent API Accept Job.
func (c *CommandStep) UnmarshalJSON(b []byte) error {
	// JSON is just a specific kind of YAML.
	var n yaml.Node
	if err := yaml.Unmarshal(b, &n); err != nil {
		return err
	}
	return ordered.Unmarshal(&n, &c)
}

// UnmarshalOrdered unmarshals a command step from an ordered map.
func (c *CommandStep) UnmarshalOrdered(src any) error {
	type wrappedCommand CommandStep
	// Unmarshal into this secret type, then process special fields specially.
	fullCommand := new(struct {
		Command  []string `yaml:"command"`
		Commands []string `yaml:"commands"`

		// Use inline trickery to capture the rest of the struct.
		Rem *wrappedCommand `yaml:",inline"`
	})
	fullCommand.Rem = (*wrappedCommand)(c)
	if err := ordered.Unmarshal(src, fullCommand); err != nil {
		return fmt.Errorf("unmarshalling CommandStep: %w", err)
	}

	// Normalise cmds into one single command string.
	// This makes signing easier later on - it's easier to hash one
	// string consistently than it is to pick apart multiple strings
	// in a consistent way in order to hash all of them
	// consistently.
	cmds := append(fullCommand.Command, fullCommand.Commands...)
	c.Command = strings.Join(cmds, "\n")
	return nil
}

// SignedFields returns the default fields for signing.
func (c *CommandStep) SignedFields() (map[string]any, error) {
	out := map[string]any{
		"command": c.Command,
		"plugins": c.Plugins,
		"matrix":  c.Matrix,
	}
	// Steps can have their own env. These can be overridden by the pipeline!
	for e, v := range c.Env {
		out[EnvNamespacePrefix+e] = v
	}
	return out, nil
}

// ValuesForFields returns the contents of fields to sign.
func (c *CommandStep) ValuesForFields(fields []string) (map[string]any, error) {
	// Make a set of required fields. As fields is processed, mark them off by
	// deleting them.
	required := map[string]struct{}{
		"command": {},
		"plugins": {},
		"matrix":  {},
	}
	// Env vars that the step has, but the pipeline doesn't have, are required.
	// But we don't know what the pipeline has without passing it in, so treat
	// all step env vars as required.
	for e := range c.Env {
		required[EnvNamespacePrefix+e] = struct{}{}
	}

	out := make(map[string]any, len(fields))
	for _, f := range fields {
		delete(required, f)

		switch f {
		case "command":
			out["command"] = c.Command

		case "plugins":
			out["plugins"] = c.Plugins

		case "matrix":
			out["matrix"] = c.Matrix

		default:
			if e, has := strings.CutPrefix(f, EnvNamespacePrefix); has {
				// Env vars requested in `fields`, but are not in this step, are
				// skipped.
				if v, ok := c.Env[e]; ok {
					out[f] = v
				}
				break
			}

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

func (c *CommandStep) interpolate(tf stringTransformer) error {
	cmd, err := tf.Transform(c.Command)
	if err != nil {
		return err
	}

	if err := interpolateSlice(tf, c.Plugins); err != nil {
		return err
	}

	if err := interpolateMap(tf, c.Env); err != nil {
		return err
	}

	// NB: Do not interpolate Signature.

	if c.Matrix, err = interpolateAny(tf, c.Matrix); err != nil {
		return err
	}

	if err := interpolateMap(tf, c.RemainingFields); err != nil {
		return err
	}

	c.Command = cmd
	return nil
}

func (CommandStep) stepTag() {}
