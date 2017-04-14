package shell

import (
  "bytes"
  "testing"

  "github.com/stretchr/testify/assert"
  "github.com/buildkite/agent/shell"
)

type testCase struct {
  secrets []string
  input string
  expectedOutput string
}

func (tc *testCase) filteredWriteOutput(input string) string {
  var buffer bytes.Buffer

  env, _ := shell.EnvironmentFromSlice(tc.secrets)
  output := shell.FilteredOutput{&buffer, env}

  output.Write([]byte(input))

  return buffer.String()
}

func TestEmptySecrets(t *testing.T) {
  testCases := []testCase {
    {[]string{}, "Output", "Output"},
  }
  for _, test := range testCases {
    assert.Equal(t, test.expectedOutput, test.filteredWriteOutput(test.input))
  }
}

func TestPartialMatchStopsUntilSure(t *testing.T) {
  testCases := []testCase {
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre", "pre"},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre ", "pre "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre s", "pre "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre sec", "pre "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre seca", "pre seca"},
  }
  for _, test := range testCases {
    assert.Equal(t, test.expectedOutput, test.filteredWriteOutput(test.input))
  }
}

func TestPartialMatchStopsAndThenSubstitutesKey(t *testing.T) {
  testCases := []testCase {
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre", "pre"},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre ", "pre "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre s", "pre "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre sec", "pre "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre seca", "pre seca"},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret", "pre $KEY1"},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1", "pre $KEY1"},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 ", "pre $KEY1 "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post", "pre $KEY1 post"},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post ", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post s", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post se", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post sec", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post secr", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post secre", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post secret", "pre $KEY1 post "},
    {[]string{"KEY1=secret1","KEY2=secret2"}, "pre secret1 post secret2", "pre $KEY1 post $KEY2"},
  }
  for _, test := range testCases {
    assert.Equal(t, test.expectedOutput, test.filteredWriteOutput(test.input))
  }
}
