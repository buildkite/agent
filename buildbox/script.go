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
  "sync"
  "regexp"
)

type Process struct {
  Output string
  Pid int
  Running bool
  ExitStatus string
  command *exec.Cmd
}

// Implement the Stringer thingy
func (p Process) String() string {
  return fmt.Sprintf("Process{Pid: %d, Running: %t, ExitStatus: %s}", p.Pid, p.Running, p.ExitStatus)
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

  Logger.Infof("Starting to run script `%s` from inside %s", script, absoluteDir)

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
    process.ExitStatus = "1"

    return &process, err
  }

  process.Pid = process.command.Process.Pid
  process.Running = true

  Logger.Infof("Process is running with PID: %d", process.Pid)

  var buffer bytes.Buffer
  var w sync.WaitGroup
  w.Add(2)

  go func() {
    Logger.Debug("Starting to copy PTY to the buffer")

    // Copy the pty to our buffer. This will block until it EOF's
    // or something breaks.
    _, err = io.Copy(&buffer, pty)
    if err != nil {
      Logger.Errorf("io.Copy failed with error: %T: %v", err, err)
    }

    w.Done()
  }()

  go func(){
    for process.Running {
      Logger.Debug("Copying buffer to the process output")

      // Convert the stdout buffer to a string
      process.Output = buffer.String()

      // Call the callback and pass in our process object
      callback(process)

      // Sleep for 1 second
      time.Sleep(1000 * time.Millisecond)
    }

    w.Done()
  }()

  // Wait until the process has finished. The returned error is nil if the command runs,
  // has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
  waitResult := process.command.Wait()

  // The process is no longer running at this point
  process.Running = false

  // Determine the exit status (if waitResult is an error, that means that the process
  // returned a non zero exit status)
  if waitResult != nil {
    if werr, ok := waitResult.(*exec.ExitError); ok {
      // This returns a string like: `exit status 123`
      exitString := werr.Error()
      exitStringRegex := regexp.MustCompile(`([0-9]+)$`)

      if exitStringRegex.MatchString(exitString) {
        process.ExitStatus = exitStringRegex.FindString(exitString)
      } else {
        Logger.Errorf("Weird looking exit status: %s", exitString)

        // If the exit status isn't what I'm looking for, provide a generic one.
        process.ExitStatus = "-1"
      }
    } else {
      Logger.Errorf("Could not determine exit status. %T: %v", waitResult, waitResult)

      // Not sure what to provide as an exit status if one couldn't be determined.
      process.ExitStatus = "-1"
    }
  } else {
    process.ExitStatus = "0"
  }

  Logger.Debugf("Process with PID: %d finished with Exit Status: %s", process.Pid, process.ExitStatus)

  Logger.Debug("Waiting for io.Copy and incremental output to finish")
  w.Wait()

  // Copy the final output back to the process
  process.Output = buffer.String()

  // No error occured so we can return nil
  return &process, nil
}
