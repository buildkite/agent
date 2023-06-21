package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Because yaml (like json) duck-types its decoding target to figure out whether
// or not to call a custom unmarshaling method, there's no other code that will
// fail to compile if these don't satisfy the interface.
var _ yaml.Unmarshaler = (*Steps)(nil)

var (
	// Returned when a pipeline has no steps.
	ErrNoSteps = errors.New("pipeline has no steps")
)

// Pipeline models a pipeline.
//
// Standard caveats apply - see the package comment.
type Pipeline struct {
	Steps Steps             `yaml:"steps"`
	Env   map[string]string `yaml:"env,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals a pipeline to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (p *Pipeline) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"steps": p.Steps,
	}
	if len(p.Env) > 0 {
		out["env"] = p.Env
	}
	for k, v := range p.RemainingFields {
		if v != nil {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

// UnmarshalYAML unmarshals a pipeline from YAML. A custom unmarshaler is
// needed since a pipeline document can either contain
//   - a sequence of steps (legacy compatibility), or
//   - a mapping with "steps" as a top-level key, that contains the steps.
func (p *Pipeline) UnmarshalYAML(n *yaml.Node) error {
	// If given a document, unwrap it first.
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) != 1 {
			return fmt.Errorf("line %d, col %d: empty document", n.Line, n.Column)
		}

		// n is now the content of the document.
		n = n.Content[0]
	}

	switch n.Kind {
	case yaml.MappingNode:
		// Hack: we want to unmarshal into Pipeline, but have it not call the
		// UnmarshalYAML method, because that would recurse infinitely...
		// Answer: Wrap Pipeline in another type.
		type pipelineWrapper Pipeline
		var q pipelineWrapper
		if err := n.Decode(&q); err != nil {
			return err
		}
		if len(q.Steps) == 0 {
			return ErrNoSteps
		}
		*p = Pipeline(q)

	case yaml.SequenceNode:
		// This sequence should be a sequence of steps.
		// No other bits (e.g. env) are present in the pipeline.
		return n.Decode(&p.Steps)

	default:
		return fmt.Errorf("line %d, col %d: unsupported YAML node kind %x for Pipeline document contents", n.Line, n.Column, n.Kind)
	}

	return nil
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
