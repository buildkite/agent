package clicommand

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMustLoadEnvFile(t *testing.T) {
	f, err := os.CreateTemp("", t.Name())
	if err != nil {
		t.Error(err)
	}
	data := map[string]string{
		"HELLO": "world",
		"FOO":   "bar\n\"bar\"\n`black hat`\r\n$(have you any root)",
	}
	for name, value := range data {
		fmt.Fprintf(f, "%s=%q\n", name, value)
	}

	result := mustLoadEnvFile(f.Name())

	assert.Equal(t, data, result, "data should round-trip via env file")
}
