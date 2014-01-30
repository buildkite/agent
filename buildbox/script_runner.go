package buildbox

import (
  "fmt"
  "time"
  "github.com/kr/pty"
  "os/exec"
  "path/filepath"
  "io"
  "bytes"
  "log"
)

type Process struct {
  Output string
  Pid int
  Running bool
  ExitStatus int
}

// Implement the Stringer thingy
func (p Process) String() string {
  return fmt.Sprintf("Process{Pid: %d, Running: %t, ExitStatus: %d}", p.Pid, p.Running, p.ExitStatus)
}

func RunScript(script string, env []string, callback func(Process)) error {
  // Create a new instance of our process struct
  var process Process

  // Build our command
  path, _ := filepath.Abs(script)
  dir := filepath.Dir(path)

  command := exec.Command(path)
  command.Dir = dir
  command.Env = env

  log.Printf("Running: %s\n", path)

  // Start our process
  pty, err := pty.Start(command)
  if err != nil {
    return err
  }

  process.Pid = command.Process.Pid
  process.Running = true

  var buffer bytes.Buffer

  go func() {
    // Copy the pty to our buffer. This will block until it EOF's
    // or something breaks.
    // TODO: What if this fails?
    io.Copy(&buffer, pty)
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
  command.Wait()

  // Update the process with the final results
  // of the script
  process.Running = false
  process.ExitStatus = 123
  process.Output = buffer.String()

  // Do the final call of the callback
  callback(process)

  // No error occured so we can return nil
  return nil
}
