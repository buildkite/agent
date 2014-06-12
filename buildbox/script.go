package buildbox

import (
  "fmt"
  "time"
  "github.com/kr/pty"
  "os/exec"
  "io"
  "os"
  "bytes"
  "path"
  "path/filepath"
  "errors"
  "syscall"
)

type Process struct {
  Output string
  Pid int
  Running bool
  ExitStatus int
  command *exec.Cmd
}

// Implement the Stringer thingy
func (p Process) String() string {
  return fmt.Sprintf("Process{Pid: %d, Running: %t, ExitStatus: %d}", p.Pid, p.Running, p.ExitStatus)
}

func (p Process) Kill() error {
  return p.command.Process.Kill()
}

func RunScript(dir string, script string, env []string, callback func(Process)) (*Process, error) {
  // Create a new instance of our process struct
  var process Process

  // Find the script to run
  absoluteDir, _ := filepath.Abs(dir)
  pathToScript := path.Join(absoluteDir, script)

  Logger.Infof("Running script `%s` from inside %s\n", script, absoluteDir)

  process.command = exec.Command(pathToScript)
  process.command.Dir = absoluteDir

  // Copy the current processes ENV and merge in the
  // new ones. We do this so the sub process gets PATH
  // and stuff.
  // TODO: Is this correct?
  currentEnv := os.Environ()
  process.command.Env = append(currentEnv, env...)

  // Start our process
  pty, err := pty.Start(process.command)
  if err != nil {
    // The process essentially failed, so we'll just make up
    // and exit status.
    process.ExitStatus = 1

    return &process, err
  }

  process.Pid = process.command.Process.Pid
  process.Running = true

  var buffer bytes.Buffer

  go func() {
    // Copy the pty to our buffer. This will block until it EOF's
    // or something breaks.
    _, err = io.Copy(&buffer, pty)
    if err != nil {
      Logger.Errorf("io.Copy failed with error: %s\n", err)
    }
  }()

  go func(){
    for process.Running {
      // Convert the stdout buffer to a string
      process.Output = buffer.String()

      // Call the callback and pass in our process object
      callback(process)

      // Sleep for 1 second
      time.Sleep(1000 * time.Millisecond)
    }
  }()

  // Wait until the process has finished
  waitResult := process.command.Wait()

  // Update the process with the final results
  // of the script
  process.Running = false
  process.ExitStatus = getExitStatus(waitResult)
  process.Output = buffer.String()

  // No error occured so we can return nil
  return &process, nil
}

// https://github.com/hnakamur/commango/blob/fe42b1cf82bf536ce7e24dceaef6656002e03743/os/executil/executil.go#L29
// WTF? Computers... (shrug)
// TODO: Can this be better?
func getExitStatus(waitResult error) int {
  if waitResult != nil {
    if err, ok := waitResult.(*exec.ExitError); ok {
      if s, ok := err.Sys().(syscall.WaitStatus); ok {
        return s.ExitStatus()
      } else {
        panic(errors.New("Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus."))
      }
    }
  }
  return 0
}
