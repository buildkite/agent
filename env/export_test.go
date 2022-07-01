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

	assertEqualEnv(t, `SOMETHING`, `0`, env)
	assertEqualEnv(t, `USER`, `keithpitt`, env)
	assertEqualEnv(t, `VAR1`, "boom\\nboom\\nshake\\nthe\\nroom", env)
	assertEqualEnv(t, `VAR2`, "hello\nfriends", env)
	assertEqualEnv(t, `VAR3`, "hello\nfriends\nOMG=foo\ntest", env)
	assertEqualEnv(t, `VAR4`, `ends with a space `, env)
	assertEqualEnv(t, `VAR5`, "ends with\nanother space ", env)
	assertEqualEnv(t, `VAR6`, "ends with a quote \"\nand a new line \"", env)
	assertEqualEnv(t, `_`, `/usr/local/bin/watch`, env)
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

	assertEqualEnv(t, `DOLLARS`, `i love $money`, env)
	assertEqualEnv(t, `WITH_NEW_LINE`, `i have a \n new line`, env)
	assertEqualEnv(t, `CARRIAGE_RETURN`, `i have a \r carriage`, env)
	assertEqualEnv(t, `TOTES`, `with a " quote`, env)
	assertEqualEnv(t, `COOL_BACKTICK`, "look at this -----> ` <----- cool backtick", env)
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

	assertEqualEnv(t, "LANG", "en_US.UTF-8", env)
	assertEqualEnv(t, "LOGNAME", `buildkite-agent`, env)
	assertEqualEnv(t, "SOME_VALUE", `this is my value`, env)
	assertEqualEnv(t, "OLDPWD", ``, env)
	assertEqualEnv(t, "SOME_OTHER_VALUE", "this is my value across\nnew\nlines", env)
	assertEqualEnv(t, "OLDPWD2", ``, env)
	assertEqualEnv(t, "SPACES", `this   one has      a bunch of spaces      in it`, env)
	assertEqualEnv(t, "GONNA_TRICK_YOU", "the next line is a string\ndeclare -x WITH_A_STRING=\"I'm a string!!\n", env)
	assertEqualEnv(t, "PATH", `/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, env)
	assertEqualEnv(t, "PWD", `/`, env)
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

	assertEqualEnv(t, `DOLLARS`, "i love $money", env)
	assertEqualEnv(t, `WITH_NEW_LINE`, `i have a \n new line`, env)
	assertEqualEnv(t, `CARRIAGE_RETURN`, `i have a \r carriage`, env)
	assertEqualEnv(t, `TOTES`, `with a " quote`, env)
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

	assertEqualEnv(t, `FOO`, strings.Join(expected, "\n"), env)
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

	if !assert.Equal(t, env.Length(), 6) {
		t.FailNow()
	}

	assertEqualEnv(t, `SESSIONNAME`, "Console", env)
	assertEqualEnv(t, `SystemDrive`, "C:", env)
	assertEqualEnv(t, `SystemRoot`, "C:\\Windows", env)
	assertEqualEnv(t, `TEMP`, "C:\\Users\\IEUser\\AppData\\Local\\Temp", env)
	assertEqualEnv(t, `TMP`, "C:\\Users\\IEUser\\AppData\\Local\\Temp", env)
	assertEqualEnv(t, `USERDOMAIN`, "IE11WIN10", env)
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

	if !assert.Equal(t, env.Length(), 6) {
		t.FailNow()
	}

	assertEqualEnv(t, `SESSIONNAME`, "Console", env)
	assertEqualEnv(t, `SystemDrive`, "C:", env)
	assertEqualEnv(t, `SystemRoot`, "C:\\Windows", env)
	assertEqualEnv(t, `TEMP`, "C:\\Users\\IEUser\\AppData\\Local\\Temp", env)
	assertEqualEnv(t, `TMP`, "C:\\Users\\IEUser\\AppData\\Local\\Temp", env)
	assertEqualEnv(t, `USERDOMAIN`, "IE11WIN10", env)
}

func assertEqualEnv(t *testing.T, key string, expected string, env Environment) {
	t.Helper()
	v, _ := env.Get(key)
	if !assert.Equal(t, expected, v) {
		t.FailNow()
	}
}
