package dockerbootstrap

import "os/exec"

// The command rejects non-Linux hosts. This stub keeps the agent portable.
func isolateProcess(cmd *exec.Cmd) {}
