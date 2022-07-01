package env

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromExportHandlesNewlines(t *testing.T) {
	lines := []string{
		`declare -x USER="keithpitt"`,
		`declare -x VAR1="boom\nboom\nshake\nthe\nroom"`,
		`declare -x VAR2="hello`,
		`friends"`,
		`declare -x VAR3="hello`,
		`friends`,
		`OMG=foo`,
		`test"`,
		`declare -x SOMETHING="0"`,
		`declare -x VAR4="ends with a space "`,
		`declare -x VAR5="ends with`,
		`another space "`,
		`declare -x VAR6="ends with a quote \"`,
		`and a new line \""`,
		`declare -x _="/usr/local/bin/watch"`,
	}

	env := FromExport(strings.Join(lines, "\n"))

	assert.Equal(t, Environment{
		"SOMETHING": `0`,
		"USER":      `keithpitt`,
		"VAR1":      "boom\\nboom\\nshake\\nthe\\nroom",
		"VAR2":      "hello\nfriends",
		"VAR3":      "hello\nfriends\nOMG=foo\ntest",
		"VAR4":      `ends with a space `,
		"VAR5":      "ends with\nanother space ",
		"VAR6":      "ends with a quote \"\nand a new line \"",
		"_":         `/usr/local/bin/watch`,
	}, env)
}

func TestFromExportHandlesEscapedCharacters(t *testing.T) {
	lines := []string{
		`declare -x DOLLARS="i love \$money"`,
		`declare -x WITH_NEW_LINE="i have a \\n new line"`,
		`declare -x CARRIAGE_RETURN="i have a \\r carriage"`,
		`declare -x TOTES="with a \" quote"`,
		"declare -x COOL_BACKTICK=\"look at this -----> \\` <----- cool backtick\"",
	}

	env := FromExport(strings.Join(lines, "\n"))

	assert.Equal(t, Environment{
		"DOLLARS":         `i love $money`,
		"WITH_NEW_LINE":   `i have a \n new line`,
		"CARRIAGE_RETURN": `i have a \r carriage`,
		"TOTES":           `with a " quote`,
		"COOL_BACKTICK":   "look at this -----> ` <----- cool backtick",
	}, env)
}

func TestFromExport_IgnoresArrays_Links_Refs_AndIntegers(t *testing.T) {
	lines := []string{
		`declare -ax COLOURS=("red" "green" "blue")`,             // Indexed array
		`declare -Ax SCORES=(["keith"]=100 ["other_keith"]=200)`, // Associative array
		`declare -nx REF=HELLO`,                                  // Reference variable
		`declare -ix TIMS_SCORE=500`,                             // Integer variable
		`declare -x HELLO="there"`,                               // Nice, normal string variable. We like this one, keep it around.
	}

	env := FromExport(strings.Join(lines, "\n"))
	assert.Equal(t, Environment{"HELLO": "there"}, env)
}

func TestFromExport_AllowsWeirdoVariableTypes_WhenTheyreInsideMultilineVars(t *testing.T) {
	scriptVariable := `#!/bin/bash
declare -ax COLOURS=("red" "green" "blue"),
declare -Ax SCORES=(["keith"]=100 ["other_keith"]=200),
declare -nx REF=HELLO,
declare -ix TIMS_SCORE=500,
	`

	lines := []string{
		fmt.Sprintf(`declare -x COOL_SCRIPT="%s"`, scriptVariable),
		`declare -x HELLO="there"`,
	}

	env := FromExport(strings.Join(lines, "\n"))
	assert.Equal(t, Environment{
		"HELLO":       "there",
		"COOL_SCRIPT": scriptVariable,
	}, env)
}

func TestFromExportWithVariablesWithoutEquals(t *testing.T) {
	lines := []string{
		`declare -x THING_TOTES`,
		`declare -x HTTP_PROXY="http://proxy.example.com:1234/"`,
		`declare -x LANG="en_US.UTF-8"`,
		`declare -x LOGNAME="buildkite-agent"`,
		`declare -x SOME_VALUE="this is my value"`,
		`declare -x OLDPWD`,
		`declare -x SOME_OTHER_VALUE="this is my value across`,
		`new`,
		`lines"`,
		`declare -x OLDPWD2`,
		`declare -x SPACES="this   one has      a bunch of spaces      in it"`,
		`declare -x GONNA_TRICK_YOU="the next line is a string`,
		`declare -x WITH_A_STRING="I'm a string!!`,
		`"`,
		`declare -x PATH="/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"`,
		`declare -x PWD="/"`,
	}

	env := FromExport(strings.Join(lines, "\n"))

	assert.Equal(t, Environment{
		"THING_TOTES":      "",
		"HTTP_PROXY":       "http://proxy.example.com:1234/",
		"LANG":             "en_US.UTF-8",
		"LOGNAME":          `buildkite-agent`,
		"SOME_VALUE":       `this is my value`,
		"OLDPWD":           ``,
		"SOME_OTHER_VALUE": "this is my value across\nnew\nlines",
		"SPACES":           `this   one has      a bunch of spaces      in it`,
		"OLDPWD2":          ``,
		"GONNA_TRICK_YOU":  "the next line is a string\ndeclare -x WITH_A_STRING=\"I'm a string!!\n",
		"PATH":             `/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`,
		"PWD":              `/`,
	}, env)
}

func TestFromExportWithLeadingAndTrailingWhitespace(t *testing.T) {
	lines := []string{
		``,
		``,
		``,
		`declare -x DOLLARS="i love \$money"`,
		`declare -x WITH_NEW_LINE="i have a \\n new line"`,
		`declare -x CARRIAGE_RETURN="i have a \\r carriage"`,
		`declare -x TOTES="with a \" quote"`,
		``,
		``,
		``,
		``,
	}
	env := FromExport(strings.Join(lines, "\n"))

	if env.Length() != 4 {
		t.Fatalf("Expected length of 4, got %d", env.Length())
	}

	assert.Equal(t, Environment{
		"DOLLARS":         "i love $money",
		"WITH_NEW_LINE":   `i have a \n new line`,
		"CARRIAGE_RETURN": `i have a \r carriage`,
		"TOTES":           `with a " quote`,
	}, env)
}

func TestFromExportJSONInside(t *testing.T) {
	lines := []string{
		`declare -x FOO="{`,
		`	\"key\": \"test\",`,
		`	\"hello\": [`,
		`	  1,`,
		`	  2,`,
		`	  3`,
		`	]`,
		`  }"`,
	}

	var expected = []string{
		`{`,
		`	"key": "test",`,
		`	"hello": [`,
		`	  1,`,
		`	  2,`,
		`	  3`,
		`	]`,
		`  }`,
	}

	env := FromExport(strings.Join(lines, "\n"))

	assert.Equal(t, Environment{"FOO": strings.Join(expected, "\n")}, env)
}

func TestFromExportFromWindows(t *testing.T) {
	lines := []string{
		`SESSIONNAME=Console`,
		`SystemDrive=C:`,
		`SystemRoot=C:\Windows`,
		`TEMP=C:\Users\IEUser\AppData\Local\Temp`,
		`TMP=C:\Users\IEUser\AppData\Local\Temp`,
		`USERDOMAIN=IE11WIN10`,
	}

	env := FromExport(strings.Join(lines, "\r\n"))

	assertEnvHas(t, env, "SESSIONNAME", "Console")
	assertEnvHas(t, env, "SystemDrive", "C:")
	assertEnvHas(t, env, "SystemRoot", `C:\Windows`)
	assertEnvHas(t, env, "TEMP", `C:\Users\IEUser\AppData\Local\Temp`)
	assertEnvHas(t, env, "TMP", `C:\Users\IEUser\AppData\Local\Temp`)
	assertEnvHas(t, env, "USERDOMAIN", "IE11WIN10")
}

func TestFromExportFromWindowsWithLeadingAndTrailingSpaces(t *testing.T) {
	lines := []string{
		``,
		``,
		``,
		`SESSIONNAME=Console`,
		`SystemDrive=C:`,
		`SystemRoot=C:\Windows`,
		`TEMP=C:\Users\IEUser\AppData\Local\Temp`,
		`TMP=C:\Users\IEUser\AppData\Local\Temp`,
		`USERDOMAIN=IE11WIN10`,
		``,
		``,
		``,
	}

	env := FromExport(strings.Join(lines, "\r\n"))

	assertEnvHas(t, env, "SESSIONNAME", "Console")
	assertEnvHas(t, env, "SystemDrive", "C:")
	assertEnvHas(t, env, "SystemRoot", `C:\Windows`)
	assertEnvHas(t, env, "TEMP", `C:\Users\IEUser\AppData\Local\Temp`)
	assertEnvHas(t, env, "TMP", `C:\Users\IEUser\AppData\Local\Temp`)
	assertEnvHas(t, env, "USERDOMAIN", "IE11WIN10")
}

// How we case the environment is different per platform - on linux we leave it alone, on windows we upcase it
// So when we're testing windowsy env vars (ie, ones that might be not all uppercase), test using env.Get(), which
// normalises everything for us
func assertEnvHas(t *testing.T, env Environment, key string, expectedVal string) {
	t.Helper()
	v, _ := env.Get(key)
	assert.Equal(t, expectedVal, v)
}
