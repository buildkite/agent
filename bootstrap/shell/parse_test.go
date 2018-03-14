package shell_test

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
)

func TestShellParsing(t *testing.T) {
	var testCases = []struct {
		String   string
		Expected []string
	}{
		{`/usr/bin/bash -e -c "llamas rock"`, []string{
			`/usr/bin/bash`, `-e`, `-c`, `llamas rock`,
		}},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			parsed, err := shell.Parse(tc.String)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(parsed, tc.Expected) {
				t.Fatalf("Expected %#v, got %#v", tc.Expected, parsed)
			}
		})
	}
}

func TestShellParsingInWindows(t *testing.T) {
	if runtime.GOOS != `windows` {
		t.Skipf("Only tested on windows")
	}

	var testCases = []struct {
		String   string
		Expected []string
	}{
		{`/usr/bin/bash -e -c "llamas rock"`, []string{
			`/usr/bin/bash`, `-e`, `-c`, `llamas rock`,
		}},
		{`C:\Windows\System32\CMD.exe /S /C "bash.exe ./upload.sh"`, []string{
			`C:/Windows/System32/CMD.exe`, `/S`, `/C`, `bash.exe ./upload.sh`,
		}},
		{`\\myuncpath\drive\buildkite-agent.exe bootstrap`, []string{
			`//myuncpath/drive/buildkite-agent.exe`, `bootstrap`,
		}},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			parsed, err := shell.Parse(tc.String)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(parsed, tc.Expected) {
				t.Fatalf("Expected %#v, got %#v", tc.Expected, parsed)
			}
		})
	}
}
