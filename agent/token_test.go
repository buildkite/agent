package agent

import (
	"flag"
	"fmt"
	"os"
	"testing"
)

func TestStringTokenErrorsWhenEmpty(t *testing.T) {
	tok, err := StringToken("").Read()

	if err != errEmptyToken {
		t.Fatalf("Read should fail when token empty")
	}

	if tok != "" {
		t.Fatalf("Read should return empty string when token empty")
	}
}

func TestScriptTokenReturnsOutput(t *testing.T) {
	st := ScriptToken{
		name: os.Args[0],
		args: []string{"-scripttoken:output=llamasrock", "-test.run=TestMainForAgentTokenScript"},
	}

	tok, err := st.Read()

	if err != nil {
		t.Fatal(err)
	}

	if tok != "llamasrock" {
		t.Fatalf("Expected command output to be llamasrock, got %q", tok)
	}
}

var scriptTokenOutput string

func init() {
	flag.StringVar(&scriptTokenOutput, "scripttoken:output", "", "custom script-token output")
	flag.Parse()
}

// This is a hack that uses the compiled test binary as a script for testing script-token, as
// such, it's not actually a test
func TestMainForAgentTokenScript(t *testing.T) {
	if scriptTokenOutput == "" {
		return
	}
	defer os.Exit(0)
	fmt.Printf(scriptTokenOutput)
}
