package pipeline

import (
	"errors"
	"io"
	"strings"

	"github.com/buildkite/agent/v3/internal/ordered"
	"gopkg.in/yaml.v3"
)

// Parse parses a pipeline. It does not apply interpolation.
func Parse(src io.Reader) (*Pipeline, error) {
	// First get yaml.v3 to give us a raw document (*yaml.Node).
	n := new(yaml.Node)
	if err := yaml.NewDecoder(src).Decode(n); err != nil {
		return nil, formatYAMLError(err)
	}

	// Instead of unmarshalling into structs, which is easy-ish to use but
	// doesn't work with some non YAML 1.2 features (merges), decode the
	// *yaml.Node into *ordered.Map, []any, or any (recursively).
	// This resolves aliases and merges and gives a more convenient form to work
	// with when handling different structural representations of the same
	// configuration. Then decode _that_ into a pipeline.
	p := new(Pipeline)
	return p, ordered.Unmarshal(n, p)
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}
