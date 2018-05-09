package run

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var (
	// 'official-plugin' and 'official-plugin#v2'
	officialPluginRegex = regexp.MustCompile(`^([A-Za-z0-9-]+)(#.+)?$`)

	// 'some-org/some-plugin' and 'some-org/some-plugin#v2'
	githubPluginRegex = regexp.MustCompile(`^([A-Za-z0-9-]+\/[A-Za-z0-9-]+)(#.+)?$`)
)

type Plugin struct {
	Name   string
	Params map[string]interface{}
}

func (p Plugin) Repository() string {
	if m := officialPluginRegex.FindStringSubmatch(p.Name); len(m) == 3 {
		return fmt.Sprintf(`github.com/buildkite-plugins/%s-buildkite-plugin%s`, m[1], m[2])
	}

	if m := githubPluginRegex.FindStringSubmatch(p.Name); len(m) == 3 {
		return fmt.Sprintf(`github.com/%s-buildkite-plugin%s`, m[1], m[2])
	}

	return p.Name
}

// The bootstrap expects an array of plugins like [{"plugin1#v1.0.0":{...}}, {"plugin2#v1.0.0":{...}}]
func marshalPlugins(plugins []Plugin) (string, error) {
	var p []interface{}

	for _, plugin := range plugins {
		p = append(p, map[string]interface{}{
			plugin.Repository(): plugin.Params,
		})
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", nil
	}

	return string(b), nil
}
