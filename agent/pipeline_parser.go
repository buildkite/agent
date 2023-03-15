package agent

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/yamltojson"
	"github.com/buildkite/interpolate"

	"gopkg.in/yaml.v3"
)

// PipelineParser parses a pipeline, optionally interpolating values from
// a given environment.
type PipelineParser struct {
	Env             *env.Environment
	Filename        string
	Pipeline        []byte
	NoInterpolation bool
}

// Parse runs the parser.
func (p *PipelineParser) Parse() (*PipelineParserResult, error) {
	if p.Env == nil {
		p.Env = env.New()
	}

	errPrefix := "Failed to parse pipeline"
	if p.Filename != "" {
		errPrefix = fmt.Sprintf("Failed to parse %s", p.Filename)
	}

	// Run the pipeline through the YAML parser. Parse to a *yaml.Node because
	// that will accept anything, but means we have more work to do.
	var doc yaml.Node
	if err := yaml.Unmarshal(p.Pipeline, &doc); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, formatYAMLError(err))
	}

	// Some quick paranoia checks...
	// yaml.v3 parses the top level as a DocumentNode, and only parses one child
	// node (that then has the content we care about).
	if doc.Kind != yaml.DocumentNode {
		// TODO: Use %v once yaml.Kind has a String method
		return nil, fmt.Errorf("%s: line %d, col %d: pipeline is not a YAML document? (kind = %x)", errPrefix, doc.Line, doc.Column, doc.Kind)
	}
	if len(doc.Content) != 1 {
		return nil, fmt.Errorf("%s: line %d, col %d: pipeline document contains %d top-level nodes, want 1", errPrefix, doc.Line, doc.Column, len(doc.Content))
	}

	// It's more useful to deal with the top-level mapping node than the
	// document node.
	pipeline := doc.Content[0]

	// We support top-level arrays of steps. If the document content is
	// a sequence node, hoist that out into a mapping node under the key "steps"
	// to make it look like a modern pipeline.
	if pipeline.Kind == yaml.SequenceNode {
		steps := pipeline
		pp, err := yamltojson.UpsertItem(nil, "steps", steps)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", errPrefix, err)
		}
		pipeline = pp
	}

	// No interpolation? No more to do.
	if p.NoInterpolation {
		return &PipelineParserResult{pipeline: pipeline}, nil
	}

	// Find the env map, if present.
	envMap, err := yamltojson.LookupItem(pipeline, "env")
	if err != nil && err != yamltojson.ErrNotFound {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if envMap != nil && envMap.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: line %d, col %d: top-level env block is not a map", errPrefix, envMap.Line, envMap.Column)
	}

	// Propagate distributed tracing context to the new pipelines if available
	if tracing, has := p.Env.Get(tracetools.EnvVarTraceContextKey); has {
		em, err := yamltojson.UpsertItem(envMap, tracetools.EnvVarTraceContextKey, yamltojson.StringNode(tracing))
		if err != nil {
			return nil, fmt.Errorf("Couldn't propagate distributed tracing context to the new pipeline: %v", err)
		}

		// Insert the new envMap into the pipeline in case it was nil before
		if envMap == nil {
			if _, err := yamltojson.UpsertItem(pipeline, "env", em); err != nil {
				return nil, fmt.Errorf("Couldn't insert environment block into to the new pipeline: %v", err)
			}
		}
		envMap = em
	}

	// Preprocess any env that are defined in the top level block and place them
	// into env for later interpolation into env blocks
	if err := p.interpolateEnvBlock(envMap); err != nil {
		return nil, err
	}

	// Recursively go through the entire pipeline and perform environment
	// variable interpolation on strings. Interpolation is performed in-place.
	if err := p.interpolateNode(pipeline); err != nil {
		return nil, err
	}

	return &PipelineParserResult{pipeline: pipeline}, nil
}

// interpolateEnvBlock runs Interpolate on each string value in the envMap,
// interpolating with the variables defined in p.Env, and then adding the
// results back into to p.Env.
func (p *PipelineParser) interpolateEnvBlock(envMap *yaml.Node) error {
	return yamltojson.RangeMap(envMap, func(k string, v *yaml.Node) error {
		if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
			return nil
		}
		interped, err := interpolate.Interpolate(p.Env, v.Value)
		if err != nil {
			return err
		}
		p.Env.Set(k, interped)
		return nil
	})
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}

// interpolateNode interpolates the YAML in-place.
func (p *PipelineParser) interpolateNode(n *yaml.Node) error {
	if n == nil {
		return nil
	}

	switch n.Kind {
	case yaml.AliasNode:
		// Ignore; every node should be reachable without following aliases.

	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		// Interpolate everything. Elements, keys, values, ...
		for _, e := range n.Content {
			if err := p.interpolateNode(e); err != nil {
				return err
			}
		}

	case yaml.ScalarNode:
		// Only interpolate strings.
		if n.Tag != "!!str" {
			return nil
		}
		interped, err := interpolate.Interpolate(p.Env, n.Value)
		if err != nil {
			return err
		}
		n.Value = interped

	default:
		return fmt.Errorf("line %d, col %d: unsupported node kind %x", n.Line, n.Column, n.Kind)
	}

	return nil
}

// PipelineParserResult is the ordered parse tree of a Pipeline document.
type PipelineParserResult struct {
	pipeline *yaml.Node
}

func (p *PipelineParserResult) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := yamltojson.Encode(&buf, p.pipeline); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
