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
	Command   string            `yaml:"command"`
	Plugins   Plugins           `yaml:"plugins,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Signature *Signature        `yaml:"signature,omitempty"`
	Matrix    any               `yaml:"matrix,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (c *CommandStep) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(c)
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
	out := map[string]string{
		"command": c.Command,
		"plugins": plugins,
	}
	// Steps can have their own env. These can be overridden by the pipeline!
	for e, v := range c.Env {
		out[EnvNamespacePrefix+e] = v
	}
	return out, nil
}

// ValuesForFields returns the contents of fields to sign.
func (c *CommandStep) ValuesForFields(fields []string) (map[string]string, error) {
	// Make a set of required fields. As fields is processed, mark them off by
	// deleting them.
	required := map[string]struct{}{
		"command": {},
		"plugins": {},
	}
	// Env vars that the step has, but the pipeline doesn't have, are required.
	// But we don't know what the pipeline has without passing it in, so treat
	// all step env vars as required.
	for e := range c.Env {
		required[EnvNamespacePrefix+e] = struct{}{}
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

func (c *CommandStep) interpolate(env interpolate.Env) error {
	cmd, err := interpolate.Interpolate(env, c.Command)
	if err != nil {
		return err
	}

	if err := interpolateSlice(env, c.Plugins); err != nil {
		return err
	}

	if err := interpolateMap(env, c.Env); err != nil {
		return err
	}

	// NB: Do not interpolate Signature.

	if c.Matrix, err = interpolateAny(env, c.Matrix); err != nil {
		return err
	}

	if err := interpolateMap(env, c.RemainingFields); err != nil {
		return err
	}

	c.Command = cmd
	return nil
}

func (CommandStep) stepTag() {}
