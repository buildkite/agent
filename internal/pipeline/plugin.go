package pipeline

import (
	"encoding/json"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/interpolate"
	"gopkg.in/yaml.v3"
)

var (
	_ interface {
		json.Marshaler
		yaml.Marshaler
		selfInterpolater
	} = (*Plugin)(nil)
)

// Plugin models plugin configuration.
//
// Standard caveats apply - see the package comment.
type Plugin struct {
	Name string

	// Config is stored in an ordered map in case any plugins are accidentally
	// relying on ordering.
	Config *ordered.MapSA
}

// MarshalJSON returns the plugin in "one-key object" form.
func (p *Plugin) MarshalJSON() ([]byte, error) {
	// NB: MarshalYAML (as seen below) never returns an error.
	o, _ := p.MarshalYAML()
	return json.Marshal(o)
}

// MarshalYAML returns the plugin in "one-item map" form.
func (p *Plugin) MarshalYAML() (any, error) {
	return map[string]*ordered.MapSA{
		p.Name: p.Config,
	}, nil
}

func (p *Plugin) interpolate(env interpolate.Env) error {
	name, err := interpolate.Interpolate(env, p.Name)
	if err != nil {
		return err
	}
	p.Name = name
	if _, err := interpolateAny(env, p.Config); err != nil {
		return err
	}
	return nil
}
