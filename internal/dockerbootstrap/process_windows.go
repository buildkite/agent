package dockerbootstrap

import "os/exec"

// Windows builds include this package, but the command rejects non-Linux hosts.
func isolateProcess(cmd *exec.Cmd) {}
