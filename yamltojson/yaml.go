package yamltojson

import (
	"fmt"

	// This is a fork of gopkg.in/yaml.v2 that fixes anchors with MapSlice
	"github.com/buildkite/yaml"
)

// Unmarshal YAML to map[string]interface{} instead of map[interface{}]interface{}, such that
// we can Marshal cleanly into JSON
// Via https://github.com/go-yaml/yaml/issues/139#issuecomment-220072190
func UnmarshalAsStringMap(in []byte, out interface{}) error {
	var res interface{}

	if err := yaml.Unmarshal(in, &res); err != nil {
		return err
	}
	*out.(*interface{}) = cleanupMapValue(res)

	return nil
}

func cleanupInterfaceArray(in []interface{}) []interface{} {
	res := make([]interface{}, len(in))
	for i, v := range in {
		res[i] = cleanupMapValue(v)
	}
	return res
}

func cleanupInterfaceMap(in map[interface{}]interface{}) map[string]interface{} {
	res := make(map[string]interface{})
	for k, v := range in {
		res[fmt.Sprintf("%v", k)] = cleanupMapValue(v)
	}
	return res
}

func cleanupMapValue(v interface{}) interface{} {
	switch v := v.(type) {
	case []interface{}:
		return cleanupInterfaceArray(v)
	case map[interface{}]interface{}:
		return cleanupInterfaceMap(v)
	case nil, bool, string, int, float64:
		return v
	default:
		panic("Unhandled map type " + fmt.Sprintf("%T", v))
	}
}
