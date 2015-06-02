package http

import (
	"encoding/json"
	"io"
	"io/ioutil"
)

type Body struct {
	reader io.ReadCloser
}

func (b *Body) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *Body) DecodeFromJSON(o interface{}) error {
	return json.NewDecoder(b).Decode(o)
}

func (b *Body) ToString() (string, error) {
	body, err := ioutil.ReadAll(b)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (b *Body) Close() error {
	return b.reader.Close()
}
