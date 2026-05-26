package archive

import "testing"

// setHomeDir overrides the home directory used by os.UserHomeDir() for the
// duration of the test. On unix-like systems os.UserHomeDir() reads $HOME,
// while on Windows it reads %USERPROFILE%; set both so callers don't have
// to care about the host platform.
func setHomeDir(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}
