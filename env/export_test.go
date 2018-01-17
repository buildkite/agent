package env

import (
	"strings"
	"testing"
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

	for k, expected := range map[string]string{
		`SOMETHING`: `0`,
		`USER`:      `keithpitt`,
		`VAR1`:      "boom\\nboom\\nshake\\nthe\\nroom",
		`VAR2`:      "hello\nfriends",
		`VAR3`:      "hello\nfriends\nOMG=foo\ntest",
		`VAR4`:      `ends with a space `,
		`VAR5`:      "ends with\nanother space ",
		`VAR6`:      "ends with a quote \"\nand a new line \"",
		`_`:         `/usr/local/bin/watch`,
	} {
		val, _ := env.Get(k)
		if val != expected {
			t.Fatalf("Expected %s to be %q, got %q", k, expected, val)
		}
	}
}

func TestFromExportHandlesEscapedCharacters(t *testing.T) {
	var lines = []string{
		`declare -x DOLLARS="i love \$money"`,
		`declare -x WITH_NEW_LINE="i have a \\n new line"`,
		`declare -x CARRIAGE_RETURN="i have a \\r carriage"`,
		`declare -x TOTES="with a \" quote"`,
	}

	env := FromExport(strings.Join(lines, "\n"))

	for k, expected := range map[string]string{
		`DOLLARS`:         `i love $money`,
		`WITH_NEW_LINE`:   `i have a \n new line`,
		`CARRIAGE_RETURN`: `i have a \r carriage`,
		`TOTES`:           `with a " quote`,
	} {
		val, _ := env.Get(k)
		if val != expected {
			t.Fatalf("Expected %s to be %q, got %q", k, expected, val)
		}
	}
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

	for k, expected := range map[string]string{
		"LANG":             "en_US.UTF-8",
		"LOGNAME":          `buildkite-agent`,
		"SOME_VALUE":       `this is my value`,
		"OLDPWD":           ``,
		"SOME_OTHER_VALUE": "this is my value across\nnew\nlines",
		"OLDPWD2":          ``,
		"GONNA_TRICK_YOU":  "the next line is a string\ndeclare -x WITH_A_STRING=\"I'm a string!!\n",
		"PATH":             `/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`,
		"PWD":              `/`,
	} {
		val, _ := env.Get(k)
		if val != expected {
			t.Fatalf("Expected %s to be %q, got %q", k, expected, val)
		}
	}
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

	if env.Length() != 4 {
		t.Fatalf("Expected length of 4, got %d", env.Length())
	}

	for k, expected := range map[string]string{
		`DOLLARS`:         "i love $money",
		`WITH_NEW_LINE`:   `i have a \n new line`,
		`CARRIAGE_RETURN`: `i have a \r carriage`,
		`TOTES`:           `with a " quote`,
	} {
		val, _ := env.Get(k)
		if val != expected {
			t.Fatalf("Expected %s to be %q, got %q", k, expected, val)
		}
	}
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

	if actual, _ := env.Get("FOO"); actual != strings.Join(expected, "\n") {
		t.Fatalf("Expected %q, got %q", actual, strings.Join(expected, "\n"))
	}
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

	if env.Length() != 6 {
		t.Fatalf("Expected length of 6, got %d", env.Length())
	}

	for k, expected := range map[string]string{
		`SESSIONNAME`: "Console",
		`SystemDrive`: "C:",
		`SystemRoot`:  "C:\\Windows",
		`TEMP`:        "C:\\Users\\IEUser\\AppData\\Local\\Temp",
		`TMP`:         "C:\\Users\\IEUser\\AppData\\Local\\Temp",
		`USERDOMAIN`:  "IE11WIN10",
	} {
		val, _ := env.Get(k)
		if val != expected {
			t.Fatalf("Expected %s to be %q, got %q", k, expected, val)
		}
	}
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

	if env.Length() != 6 {
		t.Fatalf("Expected length of 6, got %d", env.Length())
	}

	for k, expected := range map[string]string{
		`SESSIONNAME`: "Console",
		`SystemDrive`: "C:",
		`SystemRoot`:  "C:\\Windows",
		`TEMP`:        "C:\\Users\\IEUser\\AppData\\Local\\Temp",
		`TMP`:         "C:\\Users\\IEUser\\AppData\\Local\\Temp",
		`USERDOMAIN`:  "IE11WIN10",
	} {
		val, _ := env.Get(k)
		if val != expected {
			t.Fatalf("Expected %s to be %q, got %q", k, expected, val)
		}
	}
}
