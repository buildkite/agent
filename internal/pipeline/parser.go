package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/interpolate"

	"gopkg.in/yaml.v3"
)

// Parser parses a pipeline, optionally interpolating values from
// a given environment.
type Parser struct {
	Env             *env.Environment
	Filename        string
	PipelineSource  []byte
	NoInterpolation bool
}

// Parse runs the parser.
func (p *Parser) Parse() (*Pipeline, error) {
	if p.Env == nil {
		p.Env = env.New()
	}

	errPrefix := "Failed to parse pipeline"
	if p.Filename != "" {
		errPrefix = fmt.Sprintf("Failed to parse %s", p.Filename)
	}

	// Run the pipeline through the YAML parser.
	pipeline := new(Pipeline)
	if err := yaml.Unmarshal(p.PipelineSource, pipeline); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, formatYAMLError(err))
	}

	// No interpolation? No more to do.
	if p.NoInterpolation {
		return pipeline, nil
	}

	// Propagate distributed tracing context to the new pipelines if available
	if tracing, has := p.Env.Get(tracetools.EnvVarTraceContextKey); has {
		if pipeline.Env == nil {
			pipeline.Env = make(map[string]string)
		}
		pipeline.Env[tracetools.EnvVarTraceContextKey] = tracing
	}

	// Preprocess any env that are defined in the top level block and place them
	// into env for later interpolation into env blocks
	if err := p.interpolateEnvBlock(pipeline.Env); err != nil {
		return nil, err
	}

	// Recursively go through the entire pipeline and perform environment
	// variable interpolation on strings. Interpolation is performed in-place.
	if err := p.interpolatePipeline(pipeline); err != nil {
		return nil, err
	}

	return pipeline, nil
}

// interpolateEnvBlock runs Interpolate on each string value in the envMap,
// interpolating with the variables defined in p.Env, and then adding the
// results back into to p.Env.
func (p *Parser) interpolateEnvBlock(env map[string]string) error {
	for k, v := range env {
		interped, err := interpolate.Interpolate(p.Env, v)
		if err != nil {
			return err
		}
		p.Env.Set(k, interped)
	}
	return nil
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}

func (p *Parser) interpolatePipeline(pipeline *Pipeline) error {
	// interped, err := interpolate.Interpolate(p.Env, )
	// if err != nil {
	// 	return err
	// }
	// n.Value = interped
	return nil
}
