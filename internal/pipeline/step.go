package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
	"gopkg.in/yaml.v3"
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

// UnmarshalYAML unmarshals the command step. Special handling is needed to
// normalise the input (e.g. changing "commands" to "command").
func (c *CommandStep) UnmarshalYAML(n *yaml.Node) error {
	// Unmarshal into this secret type, then normalise.
	var full struct {
		// "command" and "commands" are two ways to spell the same thing.
		// They can both be single strings, or sequences of strings.
		Command   ordered.Strings `yaml:"command"`
		Commands  ordered.Strings `yaml:"commands"`
		Plugins   Plugins         `yaml:"plugins"`
		Signature *Signature      `yaml:"signature"`

		RemainingFields map[string]any `yaml:",inline"`
	}
	if err := n.Decode(&full); err != nil {
		return err
	}

	// Normalise command and commands into one single command string.
	// This makes signing easier later on - it's easier to hash one string
	// consistently than it is to pick apart multiple strings in a consistent
	// way in order to hash all of them consistently.
	c.Command = strings.Join(append(full.Command, full.Commands...), "\n")

	// Copy remaining fields.
	c.Plugins = full.Plugins
	c.Signature = full.Signature
	c.RemainingFields = full.RemainingFields
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

func (WaitStep) stepTag() {}

// InputStep models a block or input step.
//
// Standard caveats apply - see the package comment.
type InputStep map[string]any

func (s InputStep) interpolate(env interpolate.Env) error {
	return interpolateMap(env, s)
}

func (InputStep) stepTag() {}

// TriggerStep models a trigger step.
//
// Standard caveats apply - see the package comment.
type TriggerStep map[string]any

func (s TriggerStep) interpolate(env interpolate.Env) error {
	return interpolateMap(env, s)
}

func (TriggerStep) stepTag() {}

// GroupStep models a group step.
//
// Standard caveats apply - see the package comment.
type GroupStep struct {
	Steps Steps `yaml:"steps"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

func (s GroupStep) interpolate(env interpolate.Env) error {
	if err := s.Steps.interpolate(env); err != nil {
		return err
	}
	return interpolateMap(env, s.RemainingFields)
}

func (GroupStep) stepTag() {}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (g *GroupStep) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"steps": g.Steps,
	}
	for k, v := range g.RemainingFields {
		if v != nil {
			out[k] = v
		}
	}
	return json.Marshal(out)
}
