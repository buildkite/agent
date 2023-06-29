package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/yamltojson"
	"gopkg.in/yaml.v3"
)

var _ signedFielder = (*CommandStep)(nil)

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
	Label     string     `yaml:"label,omitempty"`
	Plugins   Plugins    `yaml:"plugins,omitempty"`
	Signature *Signature `yaml:"signature,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals a pipeline to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (c *CommandStep) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"command": c.Command,
	}
	if c.Label != "" {
		out["label"] = c.Label
	}
	if len(c.Plugins) > 0 {
		out["plugins"] = c.Plugins
	}
	if c.Signature != nil {
		out["signature"] = c.Signature
	}
	for k, v := range c.RemainingFields {
		if v != nil {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

func (c *CommandStep) UnmarshalYAML(n *yaml.Node) error {
	// Unmarshal into this secret type, then normalise.
	var full struct {
		Command   string     `yaml:"command,omitempty"`
		Commands  []string   `yaml:"commands,omitempty"`
		Label     string     `yaml:"label,omitempty"`
		Plugins   Plugins    `yaml:"plugins,omitempty"`
		Signature *Signature `yaml:"signature,omitempty"`

		RemainingFields map[string]any `yaml:",inline"`
	}
	if err := n.Decode(&full); err != nil {
		return err
	}

	// Normalise into Command.
	c.Command = full.Command
	if c.Command == "" {
		c.Command = strings.Join(full.Commands, "\n")
	}

	// Copy remaining fields.
	c.Label = full.Label
	c.Plugins = full.Plugins
	c.Signature = full.Signature
	c.RemainingFields = full.RemainingFields
	return nil
}

func (c *CommandStep) interpolate(pr *Parser) error {
	cmd, err := pr.interpolateStr(c.Command)
	if err != nil {
		return err
	}
	label, err := pr.interpolateStr(c.Label)
	if err != nil {
		return err
	}
	if err := c.Plugins.interpolate(pr); err != nil {
		return err
	}
	// Don't interpolate Signature.
	if _, err := pr.interpolateAny(c.RemainingFields); err != nil {
		return err
	}
	c.Command = cmd
	c.Label = label
	return nil
}

func (CommandStep) stepTag() {}

// signedFields returns the contents of fields to sign.
func (c *CommandStep) signedFields(version string) (map[string]string, error) {
	switch version {
	case "v1":
		// These fields, all these fields, and only these fields.
		// The map will be sorted by Sign.
		// Do not change the contents of old versions.
		return map[string]string{
			"command": c.Command,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported version %q", version)
	}
}

// WaitStep models a wait step.
//
// Standard caveats apply - see the package comment.
type WaitStep map[string]any

// MarshalJSON marshals a wait step as "wait" if w is empty, or the only key is
// "wait" and it has nil value. Otherwise it marshals as a standard map.
func (w WaitStep) MarshalJSON() ([]byte, error) {
	if len(w) <= 1 && w["wait"] == nil {
		return []byte(`"wait"`), nil
	}
	return json.Marshal(map[string]any(w))
}

func (w WaitStep) interpolate(pr *Parser) error {
	return interpolateMap(pr, w)
}

func (WaitStep) stepTag() {}

// InputStep models a block or input step.
//
// Standard caveats apply - see the package comment.
type InputStep map[string]any

func (i InputStep) interpolate(pr *Parser) error {
	return interpolateMap(pr, i)
}

func (InputStep) stepTag() {}

// TriggerStep models a trigger step.
//
// Standard caveats apply - see the package comment.
type TriggerStep map[string]any

func (t TriggerStep) interpolate(pr *Parser) error {
	return interpolateMap(pr, t)
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

func (g GroupStep) interpolate(pr *Parser) error {
	if err := g.Steps.interpolate(pr); err != nil {
		return err
	}
	return interpolateMap(pr, g.RemainingFields)
}

func (GroupStep) stepTag() {}

// MarshalJSON marshals a pipeline to JSON. Special handling is needed because
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

// unmarshalStep unmarshals a step (usually a mapping node, but possibly a
// scalar string) into the right kind of Step.
func unmarshalStep(n *yaml.Node) (Step, error) {
	switch n.Kind {
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

	case yaml.MappingNode, yaml.AliasNode:
		var step Step
		found := errors.New("found")
		err := yamltojson.RangeMap(n, func(key string, val *yaml.Node) error {
			switch key {
			case "command", "commands":
				step = new(CommandStep)

			case "wait", "waiter":
				step = make(WaitStep)

			case "block", "input", "manual":
				step = make(InputStep)

			case "trigger":
				step = make(TriggerStep)

			case "group":
				step = new(GroupStep)

			default:
				// Ignore anything not listed above.
				return nil
			}
			return found
		})
		if err != nil && err != found {
			return nil, err
		}

		if step == nil {
			return nil, fmt.Errorf("line %d, col %d: unknown step type", n.Line, n.Column)
		}

		if err := n.Decode(step); err != nil {
			return nil, err
		}
		return step, nil

	default:
		return nil, fmt.Errorf("line %d, col %d: unsupported YAML node kind %x for Step", n.Line, n.Column, n.Kind)
	}
}

// Steps contains multiple steps. It is useful for unmarshaling step sequences,
// since it has custom logic for determining the correct step type.
type Steps []Step

// UnmarshalYAML unmarshals a sequence (of steps). An empty sequence is an error.
func (s *Steps) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d, col %d: wrong node kind %v for step sequence", n.Line, n.Column, n.Kind)
	}
	if len(n.Content) == 0 {
		return ErrNoSteps
	}

	for _, c := range n.Content {
		step, err := unmarshalStep(c)
		if err != nil {
			return err
		}
		*s = append(*s, step)
	}
	return nil
}

func (s Steps) interpolate(pr *Parser) error {
	return interpolateSlice(pr, s)
}
