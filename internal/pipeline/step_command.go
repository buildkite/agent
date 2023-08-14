package pipeline

import (
	"errors"
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
