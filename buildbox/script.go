package buildbox

import (
  "fmt"
  "time"
  "github.com/kr/pty"
  "os/exec"
  "io"
  "os"
  "bytes"
  "log"
  "path"
  "path/filepath"
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

func RunScript(dir string, script string, env []string, callback func(Process)) error {
  // Create a new instance of our process struct
  var process Process

  // Find the script to run
  absoluteDir, _ := filepath.Abs(dir)
  pathToScript := path.Join(absoluteDir, script)

  log.Printf("Running: %s from within %s\n", script, absoluteDir)

  command := exec.Command(pathToScript)
  command.Dir = absoluteDir

  // Copy the current processes ENV and merge in the
  // new ones. We do this so the sub process gets PATH
  // and stuff.
  // TODO: Is this correct?
  currentEnv := os.Environ()
  command.Env = append(currentEnv, env...)

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
  // TODO: Find out how to get the correct ExitStatus
  process.Running = false
  process.ExitStatus = 123
  process.Output = buffer.String()

  // Do the final call of the callback
  callback(process)

  // No error occured so we can return nil
  return nil
}
