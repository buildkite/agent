package shellscript

import (
	"errors"
	"os"
	"testing"
)

func TestShebangLine(t *testing.T) {
	tests := []struct {
		name, contents, want string
	}{
		{
			name:     "bash",
			contents: "#!/usr/bin/env bash\necho 'Llamas!'",
			want:     "#!/usr/bin/env bash",
		},
		{
			name:     "python3",
			contents: "#!/usr/bin/env python3\nprint('Llamas!')",
			want:     "#!/usr/bin/env python3",
		},
		{
			name:     "not a script",
			contents: "Not a script\n#!what",
			want:     "",
		},
		{
			name:     "empty",
			contents: "",
			want:     "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := os.CreateTemp("", "TestShebangLine-*")
			if err != nil {
				t.Fatalf("os.CreateTemp(TestShebangLine-*) error = %v", err)
			}
			t.Cleanup(func() {
				os.Remove(f.Name()) //nolint:errcheck // File removal is best-effort cleanup.
			})

			if _, err := f.WriteString(test.contents); err != nil {
				t.Fatalf("f.WriteString(%q) error = %v", test.contents, err)
			}

			if err := f.Close(); err != nil {
				t.Fatalf("f.Close() = %v", err)
			}

			got, err := ShebangLine(f.Name())
			if err != nil {
				t.Fatalf("ShebangLine(%q) error = %v", f.Name(), err)
			}

			if got != test.want {
				t.Errorf("ShebangLine(%q) = %q, want %q", f.Name(), got, test.want)
			}
		})
	}

	t.Run("file not exist", func(t *testing.T) {
		path := "/this/file/should/not/exist"
		_, err := ShebangLine(path)
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ShebangLine(%q) error = %v, want %v", path, err, os.ErrNotExist)
		}
	})
}

func TestIsPOSIXShell(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"", false},
		{"garbage", false},
		{"/bin/sh", true},
		{"/bin/fish", false},
		{"#!/usr/bin/env bash", true},
		{"#!/bin/cat", false},
		{"#!/usr/bin/env bash", true},
		{"#!/usr/bin/env python3", false},
		{"#!/usr/bin/env", false},
	}
	for _, test := range tests {
		if got, want := IsPOSIXShell(test.line), test.want; got != want {
			t.Errorf("IsPOSIXShell(%q) = %t, want %t", test.line, got, want)
		}
	}
}
