package pipeline

import (
	"encoding/json"
	"strings"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
	"gopkg.in/yaml.v3"
)

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
	Command string  `yaml:"command"`
	Plugins Plugins `yaml:"plugins,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals the step to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (c *CommandStep) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(c.RemainingFields)+2)
	for k, v := range c.RemainingFields {
		if v != nil {
			out[k] = v
		}
	}
	if c.Command != "" {
		out["command"] = c.Command
	}
	if len(c.Plugins) > 0 {
		out["plugins"] = c.Plugins
	}

	return json.Marshal(out)
}

// UnmarshalYAML unmarshals the command step. Special handling is needed to
// normalise the input (e.g. changing "commands" to "command").
func (c *CommandStep) UnmarshalYAML(n *yaml.Node) error {
	// Unmarshal into this secret type, then normalise.
	var full struct {
		// "command" and "commands" are two ways to spell the same thing.
		// They can both be single strings, or sequences of strings.
		Command  ordered.Strings `yaml:"command"`
		Commands ordered.Strings `yaml:"commands"`
		Plugins  Plugins         `yaml:"plugins"`

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
	c.RemainingFields = full.RemainingFields
	return nil
}

func (c *CommandStep) interpolate(env interpolate.Env) error {
	cmd, err := interpolate.Interpolate(env, c.Command)
	if err != nil {
		return err
	}
	if err := c.Plugins.interpolate(env); err != nil {
		return err
	}
	if _, err := interpolateAny(env, c.RemainingFields); err != nil {
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
