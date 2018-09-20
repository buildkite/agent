package yamltojson

import (
	"bytes"
	"encoding/json"
	"fmt"

	// This is a fork of gopkg.in/yaml.v2 that fixes anchors with MapSlice
	"github.com/buildkite/yaml"
)

func MarshalMapSliceJSON(m yaml.MapSlice) ([]byte, error) {
	buffer := bytes.NewBufferString("{")
	length := len(m)
	count := 0

	for _, item := range m {
		jsonValue, err := marshalInterfaceJSON(item.Value)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(fmt.Sprintf("%q:%s", item.Key, string(jsonValue)))
		count++
		if count < length {
			buffer.WriteString(",")
		}
	}

	buffer.WriteString("}")
	return buffer.Bytes(), nil
}

func marshalSliceJSON(m []interface{}) ([]byte, error) {
	buffer := bytes.NewBufferString("[")
	length := len(m)
	count := 0

	for _, item := range m {
		jsonValue, err := marshalInterfaceJSON(item)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(fmt.Sprintf("%s", string(jsonValue)))
		count++
		if count < length {
			buffer.WriteString(",")
		}
	}

	buffer.WriteString("]")
	return buffer.Bytes(), nil
}

func marshalInterfaceJSON(i interface{}) ([]byte, error) {
	switch t := i.(type) {
	case yaml.MapItem:
		return marshalInterfaceJSON(t.Value)
	case yaml.MapSlice:
		return MarshalMapSliceJSON(t)
	case []yaml.MapItem:
		var s []interface{}
		for _, mi := range t {
			s = append(s, mi.Value)
		}
		return marshalSliceJSON(s)
	case []interface{}:
		return marshalSliceJSON(t)
	default:
		return json.Marshal(i)
	}
}
