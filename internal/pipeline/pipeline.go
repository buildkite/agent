package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
	"gopkg.in/yaml.v3"
)

// Returned when a pipeline has no steps.
var ErrNoSteps = errors.New("pipeline has no steps")

// Pipeline models a pipeline.
//
// Standard caveats apply - see the package comment.
type Pipeline struct {
	Steps ordered.Slice  `yaml:"steps"`
	Env   *ordered.MapSS `yaml:"env,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals a pipeline to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (p *Pipeline) MarshalJSON() ([]byte, error) {
	// Steps and Env have precedence over anything in RemainingFields.
	out := make(map[string]any, len(p.RemainingFields)+2)
	for k, v := range p.RemainingFields {
		if v != nil {
			out[k] = v
		}
	}

	out["steps"] = p.Steps
	if !p.Env.IsZero() {
		out["env"] = p.Env
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

func (p *Pipeline) interpolate(env interpolate.Env) error {
	if err := interpolateSlice(env, p.Steps); err != nil {
		return err
	}
	if err := interpolateOrderedMap(env, p.Env); err != nil {
		return err
	}
	return interpolateMap(env, p.RemainingFields)
}
