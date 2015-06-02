// +build !windows

package buildkite

import (
	"github.com/buildkite/agent/logger"
	"os"
	"os/signal"
	"syscall"
)

func (a *Agent) MonitorSignals() {
	// Handle signals
	signals := make(chan os.Signal, 1)

	// Monitor a heap of different signals for debugging purposes. Only
	// some of them are used. Descriptions are copied from
	// http://en.wikipedia.org/wiki/Unix_signal so when working with this
	// code, we know exactly what each signal should do (and  alwsays
	// forget the specifics)

	signal.Notify(signals, os.Interrupt,
		// The SIGHUP signal is sent to a process when its controlling
		// terminal is closed. It was originally designed to notify the
		// process of a serial line drop (a hangup). In modern systems,
		// this signal usually means that the controlling pseudo or
		// virtual terminal has been closed.[3] Many daemons will
		// reload their configuration files and reopen their logfiles
		// instead of exiting when receiving this signal.[4] nohup is a
		// command to make a command ignore the signal.
		syscall.SIGHUP,

		// The SIGTERM signal is sent to a process to request its
		// termination. Unlike the SIGKILL signal, it can be caught and
		// interpreted or ignored by the process.  This allows the
		// process to perform nice termination releasing resources and
		// saving state if appropriate. It should be noted that SIGINT
		// is nearly identical to SIGTERM.
		syscall.SIGTERM,

		// The SIGKILL signal is sent to a process to cause it to
		// terminate immediately (kill). In contrast to SIGTERM and
		// SIGINT, this signal cannot be caught or ignored, and the
		// receiving process cannot perform any clean-up upon receiving
		// this signal.
		syscall.SIGKILL,

		// The SIGINT signal is sent to a process by its controlling
		// terminal when a user wishes to interrupt the process. This
		// is typically initiated by pressing Control-C, but on some
		// systems, the "delete" character or "break" key can be
		// used.[5]
		syscall.SIGINT,

		// The SIGQUIT signal is sent to a process by its controlling
		// terminal when the user requests that the process quit and
		// perform a core dump.
		syscall.SIGQUIT,

		// The SIGUSR1 and SIGUSR2 signals are sent to a process to
		// indicate user-defined conditions. In this case SIGUSR1 will
		// show debug information and SIGUSR2 is ignored for now, but
		// in the future it may reload logs.
		syscall.SIGUSR1,
		syscall.SIGUSR2)

	go func() {
		// This will block until a signal is sent
		sig := <-signals

		logger.Debug("Received signal `%s`", sig.String())

		// If we've received a SIGKILL, die immediately.
		if sig == syscall.SIGKILL {
			logger.Debug("Exiting immediately", sig.String())

			os.Exit(1)
		}

		// Show debug information with SIGUSR1
		if sig == syscall.SIGUSR1 {
			logger.Debug("======DEBUG===== %s", a)
			if a.Job != nil {
				logger.Debug("======DEBUG===== %s", a.Job)
			}

			// Start monitoring signals again
			a.MonitorSignals()

			return
		}

		// Exit the agent when it's not doing any work.
		if sig == syscall.SIGTERM || sig == syscall.SIGINT || sig == syscall.SIGQUIT {
			// If theres no job, then we can just shut down right away.
			if a.Job == nil {
				a.Stop()
			} else {
				// The user is trying to forcefully kill the agent, so we need
				// to kill any active job.
				if a.stopping {
					a.Job.Kill()
				} else {
					// We should warn the user before they try and shut down the
					// agent while it's performing a job
					logger.Warn("Waiting for job to finish before stopping. Send the signal again to exit immediately.")

					a.stopping = true
				}
			}

			// Start monitoring signals again
			a.MonitorSignals()

			return
		}

		logger.Debug("Ignoring signal `%s`", sig.String())

		// Start monitoring signals again
		a.MonitorSignals()

		return
	}()
}
