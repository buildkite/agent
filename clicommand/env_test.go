package clicommand

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvFile(t *testing.T) {
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

	result, err := loadEnvFile(f.Name())
	require.NoError(t, err)

	assert.Equal(t, data, result, "data should round-trip via env file")
}

func TestLoadEnvFileQuotingError(t *testing.T) {
	f, err := os.CreateTemp("", t.Name())
	require.NoError(t, err)

	fmt.Fprintf(f, "%s=%q\n", "ONE", "ok")
	fmt.Fprintln(f, "TWO=missing quotes")

	result, err := loadEnvFile(f.Name())
	assert.Nil(t, result)

	assert.Equal(t, `unquoting value in `+f.Name()+`:2: invalid syntax`, err.Error())
}
