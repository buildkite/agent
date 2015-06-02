package http

import (
	"bytes"
	"encoding/json"
)

type JSON struct {
	Payload interface{}
}

func (j *JSON) ToBody() (*bytes.Buffer, error) {
	buffer := &bytes.Buffer{}
	enc := json.NewEncoder(buffer)

	err := enc.Encode(j.Payload)
	if err != nil {
		return nil, err
	}

	return buffer, nil
}

func (j *JSON) ContentType() string {
	return "application/json"
}
