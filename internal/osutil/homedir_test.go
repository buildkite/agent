package osutil

import (
	"os"
	"runtime"
	"testing"
)

func TestUserHomeDir(t *testing.T) {
	// not parallel because it messes with env vars
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)               //nolint:errcheck // Restoring $HOME is best effort.
		os.Setenv("USERPROFILE", origUserProfile) //nolint:errcheck // Restoring $USERPROFILE is best effort.
	})

	type testCase struct {
		home, userProfile, want string
	}

	tests := []testCase{
		{
			// Prefer $HOME on all platforms
			home:        "home",
			userProfile: "userProfile",
			want:        "home",
		},
	}
	if runtime.GOOS == "windows" {
		// Windows can use %USERPROFILE% as a treat when $HOME is unavailable
		tests = append(tests, testCase{
			home:        "",
			userProfile: "userProfile",
			want:        "userProfile",
		})
	}

	for _, test := range tests {
		if err := os.Setenv("HOME", test.home); err != nil {
			t.Fatalf("os.Setenv(HOME, %q) = %v", test.home, err)
		}
		if err := os.Setenv("USERPROFILE", test.userProfile); err != nil {
			t.Fatalf("os.Setenv(USERPROFILE, %q) = %v", test.home, err)
		}
		got, err := UserHomeDir()
		if err != nil {
			t.Errorf("HOME=%q USERPROFILE=%q UserHomeDir() error = %v", test.home, test.userProfile, err)
		}
		if got != test.want {
			t.Errorf("HOME=%q USERPROFILE=%q UserHomeDir() = %q, want %q", test.home, test.userProfile, got, test.want)
		}
	}
}
