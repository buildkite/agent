package env

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromExportHandlesNewlines(t *testing.T) {
	var lines = []string{
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
	assert.Equal(t, env.Get(`SOMETHING`), `0`)
	assert.Equal(t, env.Get(`USER`), `keithpitt`)
	assert.Equal(t, env.Get(`VAR1`), "boom\\nboom\\nshake\\nthe\\nroom")
	assert.Equal(t, env.Get(`VAR2`), "hello\nfriends")
	assert.Equal(t, env.Get(`VAR3`), "hello\nfriends\nOMG=foo\ntest")
	assert.Equal(t, env.Get(`VAR4`), `ends with a space `)
	assert.Equal(t, env.Get(`VAR5`), "ends with\nanother space ")
	assert.Equal(t, env.Get(`VAR6`), "ends with a quote \"\nand a new line \"")
	assert.Equal(t, env.Get(`_`), `/usr/local/bin/watch`)
}

func TestFromExportHandlesEscapedCharacters(t *testing.T) {
	var lines = []string{
		`declare -x DOLLARS="i love \$money"`,
		`declare -x WITH_NEW_LINE="i have a \\n new line"`,
		`declare -x CARRIAGE_RETURN="i have a \\r carriage"`,
		`declare -x TOTES="with a \" quote"`,
	}

	env := FromExport(strings.Join(lines, "\n"))
	assert.Equal(t, env.Get(`DOLLARS`), `i love $money`)
	assert.Equal(t, env.Get(`WITH_NEW_LINE`), `i have a \n new line`)
	assert.Equal(t, env.Get(`CARRIAGE_RETURN`), `i have a \r carriage`)
	assert.Equal(t, env.Get(`TOTES`), `with a " quote`)
}

func TestFromExportWithVariablesWithoutEquals(t *testing.T) {
	var lines = []string{
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
		`declare -x GONNA_TRICK_YOU="the next line is a string`,
		`declare -x WITH_A_STRING="I'm a string!!`,
		`"`,
		`declare -x PATH="/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"`,
		`declare -x PWD="/"`,
	}

	env := FromExport(strings.Join(lines, "\n"))

	assert.Equal(t, env.Get("LANG"), "en_US.UTF-8")
	assert.Equal(t, env.Get("LOGNAME"), `buildkite-agent`)
	assert.Equal(t, env.Get("SOME_VALUE"), `this is my value`)
	assert.Equal(t, env.Get("OLDPWD"), ``)
	assert.Equal(t, env.Get("SOME_OTHER_VALUE"), "this is my value across\nnew\nlines")
	assert.Equal(t, env.Get("OLDPWD2"), ``)
	assert.Equal(t, env.Get("GONNA_TRICK_YOU"), "the next line is a string\ndeclare -x WITH_A_STRING=\"I'm a string!!\n")
	assert.Equal(t, env.Get("PATH"), `/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`)
	assert.Equal(t, env.Get("PWD"), `/`)
}

func TestFromExportWithLeadingAndTrailingWhitespace(t *testing.T) {
	var lines = []string{
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

	assert.Equal(t, 4, env.Length())
	assert.Equal(t, "i love $money", env.Get("DOLLARS"))
	assert.Equal(t, `i have a \n new line`, env.Get("WITH_NEW_LINE"))
	assert.Equal(t, `i have a \r carriage`, env.Get("CARRIAGE_RETURN"))
	assert.Equal(t, `with a " quote`, env.Get("TOTES"))
}

func TestFromExportJSONInside(t *testing.T) {
	var lines = []string{
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
	assert.Equal(t, env.Get("FOO"), strings.Join(expected, "\n"))
}

func TestFromExportFromWindows(t *testing.T) {
	var lines = []string{
		`SESSIONNAME=Console`,
		`SystemDrive=C:`,
		`SystemRoot=C:\Windows`,
		`TEMP=C:\Users\IEUser\AppData\Local\Temp`,
		`TMP=C:\Users\IEUser\AppData\Local\Temp`,
		`USERDOMAIN=IE11WIN10`,
	}

	env := FromExport(strings.Join(lines, "\r\n"))
	assert.Equal(t, 6, env.Length())
	assert.Equal(t, env.Get("SESSIONNAME"), "Console")
	assert.Equal(t, env.Get("SystemDrive"), "C:")
	assert.Equal(t, env.Get("SystemRoot"), "C:\\Windows")
	assert.Equal(t, env.Get("TEMP"), "C:\\Users\\IEUser\\AppData\\Local\\Temp")
	assert.Equal(t, env.Get("TMP"), "C:\\Users\\IEUser\\AppData\\Local\\Temp")
	assert.Equal(t, env.Get("USERDOMAIN"), "IE11WIN10")
}

func TestFromExportFromWindowsWithLeadingAndTrailingSpaces(t *testing.T) {
	var lines = []string{
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
	assert.Equal(t, 6, env.Length())
	assert.Equal(t, env.Get("SESSIONNAME"), "Console")
	assert.Equal(t, env.Get("SystemDrive"), "C:")
	assert.Equal(t, env.Get("SystemRoot"), "C:\\Windows")
	assert.Equal(t, env.Get("TEMP"), "C:\\Users\\IEUser\\AppData\\Local\\Temp")
	assert.Equal(t, env.Get("TMP"), "C:\\Users\\IEUser\\AppData\\Local\\Temp")
	assert.Equal(t, env.Get("USERDOMAIN"), "IE11WIN10")
}
