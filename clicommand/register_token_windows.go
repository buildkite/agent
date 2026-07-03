//go:build !unix

package clicommand

// reexecToScrubRegistrationToken is a no-op on non-Unix platforms: there's no
// exec(2) equivalent to replace the process image in place, and no /proc
// exposing the exec-time command line and environment to other processes.
func reexecToScrubRegistrationToken() error {
	return nil
}
