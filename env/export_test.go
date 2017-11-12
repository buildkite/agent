package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromExport(t *testing.T) {
	// Handles new lines
	env := FromExport(`declare -x USER="keithpitt"
declare -x VAR1="boom\nboom\nshake\nthe\nroom"
declare -x VAR2="hello
friends"
declare -x VAR3="hello
friends
OMG=foo
test"
declare -x SOMETHING="0"
declare -x VAR4="ends with a space "
declare -x VAR5="ends with
another space "
declare -x VAR6="ends with a quote \"
and a new line \""
declare -x _="/usr/local/bin/watch"`)
	assert.Equal(t, []string{
		"SOMETHING=0",
		"USER=keithpitt",
		"VAR1=boom\\nboom\\nshake\\nthe\\nroom",
		"VAR2=hello\nfriends",
		"VAR3=hello\nfriends\nOMG=foo\ntest",
		"VAR4=ends with a space ",
		"VAR5=ends with\nanother space ",
		"VAR6=ends with a quote \"\nand a new line \"",
		"_=/usr/local/bin/watch",
	}, env.ToSlice())

	// Escapes stuff
	env = FromExport(`declare -x DOLLARS="i love \$money"
declare -x WITH_NEW_LINE="i have a \\n new line"
declare -x CARRIAGE_RETURN="i have a \\r carriage"
declare -x TOTES="with a \" quote"`)

	assertEqualEnv(t, "DOLLARS", "i love $money", env)
	assertEqualEnv(t, "WITH_NEW_LINE", `i have a \n new line`, env)
	assertEqualEnv(t, "CARRIAGE_RETURN", `i have a \r carriage`, env)
	assertEqualEnv(t, "TOTES", `with a " quote`, env)

	// Handles environment variables with no "=" in them
	env = FromExport(`declare -x THING_TOTES
declare -x HTTP_PROXY="http://proxy.example.com:1234/"
declare -x LANG="en_US.UTF-8"
declare -x LOGNAME="buildkite-agent"
declare -x SOME_VALUE="this is my value"
declare -x OLDPWD
declare -x SOME_OTHER_VALUE="this is my value across
new
lines"
declare -x OLDPWD2
declare -x GONNA_TRICK_YOU="the next line is a string
declare -x WITH_A_STRING="I'm a string!!
"
declare -x PATH="/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
declare -x PWD="/"
`)

	assertEqualEnv(t, "LANG", "en_US.UTF-8", env)
	assertEqualEnv(t, "LOGNAME", `buildkite-agent`, env)
	assertEqualEnv(t, "SOME_VALUE", `this is my value`, env)
	assertEqualEnv(t, "OLDPWD", ``, env)
	assertEqualEnv(t, "SOME_OTHER_VALUE", "this is my value across\nnew\nlines", env)
	assertEqualEnv(t, "OLDPWD2", ``, env)
	assertEqualEnv(t, "GONNA_TRICK_YOU", "the next line is a string\ndeclare -x WITH_A_STRING=\"I'm a string!!\n", env)
	assertEqualEnv(t, "PATH", `/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, env)
	assertEqualEnv(t, "PWD", `/`, env)

	// Disregards new lines at the start and end of the export
	env = FromExport(`



declare -x DOLLARS="i love \$money"
declare -x WITH_NEW_LINE="i have a \\n new line"
declare -x CARRIAGE_RETURN="i have a \\r carriage"
declare -x TOTES="with a \" quote"



`)

	assertEqualEnv(t, "DOLLARS", "i love $money", env)
	assertEqualEnv(t, "WITH_NEW_LINE", `i have a \n new line`, env)
	assertEqualEnv(t, "CARRIAGE_RETURN", `i have a \r carriage`, env)
	assertEqualEnv(t, "TOTES", `with a " quote`, env)

	// Handles JSON
	env = FromExport(`declare -x FOO="{
  \"key\": \"test\",
  \"hello\": [
    1,
    2,
    3
  ]
}"`)

	assertEqualEnv(t, "FOO", `{
  "key": "test",
  "hello": [
    1,
    2,
    3
  ]
}`, env)

	// Works with Windows output
	env = FromExport(`SESSIONNAME=Console
SystemDrive=C:
SystemRoot=C:\Windows
TEMP=C:\Users\IEUser\AppData\Local\Temp
TMP=C:\Users\IEUser\AppData\Local\Temp
USERDOMAIN=IE11WIN10`)

	assert.Equal(t, []string{
		"SESSIONNAME=Console",
		"SystemDrive=C:",
		"SystemRoot=C:\\Windows",
		"TEMP=C:\\Users\\IEUser\\AppData\\Local\\Temp",
		"TMP=C:\\Users\\IEUser\\AppData\\Local\\Temp",
		"USERDOMAIN=IE11WIN10",
	}, env.ToSlice())

	// Works with Windows output that has spaces at the start and end
	env = FromExport(`

SESSIONNAME=Console
SystemDrive=C:
SystemRoot=C:\Windows
TEMP=C:\Users\IEUser\AppData\Local\Temp
TMP=C:\Users\IEUser\AppData\Local\Temp
USERDOMAIN=IE11WIN10

`)

	assert.Equal(t, []string{
		"SESSIONNAME=Console",
		"SystemDrive=C:",
		"SystemRoot=C:\\Windows",
		"TEMP=C:\\Users\\IEUser\\AppData\\Local\\Temp",
		"TMP=C:\\Users\\IEUser\\AppData\\Local\\Temp",
		"USERDOMAIN=IE11WIN10",
	}, env.ToSlice())
}

func assertEqualEnv(t *testing.T, key string, expected string, env *Environment) {
	v, _ := env.Get(key)
	assert.Equal(t, expected, v)
}
