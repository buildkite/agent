package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentExists(t *testing.T) {
	env := EnvironmentFromSlice([]string{})

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	assert.Equal(t, env.Exists("FOO"), true)
	assert.Equal(t, env.Exists("EMPTY"), true)
	assert.Equal(t, env.Exists("does not exist"), false)
}

func TestEnvironmentSet(t *testing.T) {
	env := EnvironmentFromSlice([]string{})

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")
	assert.Equal(t, env.Get("    THIS_IS_THE_BEST   \n\n"), "\"IT SURE IS\"\n\n")
}

func TestEnvironmentRemove(t *testing.T) {
	env := EnvironmentFromSlice([]string{"FOO=bar"})

	assert.Equal(t, env.Get("FOO"), "bar")
	assert.Equal(t, env.Remove("FOO"), "bar")
	assert.Equal(t, env.Get(""), "")
}

func TestEnvironmentMerge(t *testing.T) {
	env1 := EnvironmentFromSlice([]string{"FOO=bar"})
	env2 := EnvironmentFromSlice([]string{"BAR=foo"})

	env3 := env1.Merge(env2)

	assert.Equal(t, env3.ToSlice(), []string{"BAR=foo", "FOO=bar"})
}

func TestEnvironmentCopy(t *testing.T) {
	env1 := EnvironmentFromSlice([]string{"FOO=bar"})
	env2 := env1.Copy()

	assert.Equal(t, []string{"FOO=bar"}, env2.ToSlice())

	env1.Set("FOO", "not-bar-anymore")

	assert.Equal(t, []string{"FOO=bar"}, env2.ToSlice())
}

func TestEnvironmentFromExport(t *testing.T) {
	// Handles new lines
	env := EnvironmentFromExport(`declare -x USER="keithpitt"
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
declare -x _="/usr/local/bin/watch"`)
	assert.Equal(t, []string{
		"SOMETHING=0",
		"USER=keithpitt",
		"VAR1=boom\\nboom\\nshake\\nthe\\nroom",
		"VAR2=hello\nfriends",
		"VAR3=hello\nfriends\nOMG=foo\ntest",
		"VAR4=ends with a space ",
		"VAR5=ends with\nanother space ",
		"_=/usr/local/bin/watch",
	}, env.ToSlice())

	// Escapes stuff
	env = EnvironmentFromExport(`declare -x DOLLARS="i love \$money"
declare -x WITH_NEW_LINE="i have a \\n new line"
declare -x CARRIAGE_RETURN="i have a \\r carriage"
declare -x TOTES="with a \" quote"`)

	assert.Equal(t, "i love $money", env.Get("DOLLARS"))
	assert.Equal(t, `i have a \n new line`, env.Get("WITH_NEW_LINE"))
	assert.Equal(t, `i have a \r carriage`, env.Get("CARRIAGE_RETURN"))
	assert.Equal(t, `with a " quote`, env.Get("TOTES"))

	// Disregards new lines at the start and end of the export
	env = EnvironmentFromExport(`



declare -x DOLLARS="i love \$money"
declare -x WITH_NEW_LINE="i have a \\n new line"
declare -x CARRIAGE_RETURN="i have a \\r carriage"
declare -x TOTES="with a \" quote"



`)

	assert.Equal(t, "i love $money", env.Get("DOLLARS"))
	assert.Equal(t, `i have a \n new line`, env.Get("WITH_NEW_LINE"))
	assert.Equal(t, `i have a \r carriage`, env.Get("CARRIAGE_RETURN"))
	assert.Equal(t, `with a " quote`, env.Get("TOTES"))

	// Handles JSON
	env = EnvironmentFromExport(`declare -x FOO="{
  \"key\": \"test\",
  \"hello\": [
    1,
    2,
    3
  ]
}"`)

	assert.Equal(t, env.Get("FOO"), `{
  "key": "test",
  "hello": [
    1,
    2,
    3
  ]
}`)

	// Works with Windows output
	env = EnvironmentFromExport(`SESSIONNAME=Console
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
	env = EnvironmentFromExport(`

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

func TestEnvironmentToSlice(t *testing.T) {
	env := EnvironmentFromSlice([]string{"THIS_IS_GREAT=totes", "ZOMG=greatness"})

	assert.Equal(t, []string{"THIS_IS_GREAT=totes", "ZOMG=greatness"}, env.ToSlice())
}
