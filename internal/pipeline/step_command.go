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

// InterpolateMatrixPermutation validates and then interpolates the choice of
// matrix values into the step. This should only be used in order to validate
// a job that's about to be run, and not used before pipeline upload.
func (c *CommandStep) InterpolateMatrixPermutation(mp MatrixPermutation) error {
	if err := c.Matrix.validatePermutation(mp); err != nil {
		return err
	}
	if len(mp) == 0 {
		return nil
	}
	return c.interpolate(newMatrixInterpolator(mp))
}

func (c *CommandStep) interpolate(tf stringTransformer) error {
	cmd, err := tf.Transform(c.Command)
	if err != nil {
		return err
	}
	c.Command = cmd

	if err := interpolateSlice(tf, c.Plugins); err != nil {
		return err
	}

	switch tf.(type) {
	case envInterpolator:
		if err := interpolateMap(tf, c.Env); err != nil {
			return err
		}
		if c.Matrix, err = interpolateAny(tf, c.Matrix); err != nil {
			return err
		}

	case matrixInterpolator:
		// Matrix interpolation doesn't apply to env keys.
		if err := interpolateMapValues(tf, c.Env); err != nil {
			return err
		}
	}

	// NB: Do not interpolate Signature.

	if err := interpolateMap(tf, c.RemainingFields); err != nil {
		return err
	}

	return nil
}

func (CommandStep) stepTag() {}
