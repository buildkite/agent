package buildbox

import (
	"os"
	"os/signal"
)

func (a *Agent) MonitorSignals() {
	// Handle signals
	signals := make(chan os.Signal, 1)

	signal.Notify(signals, os.Interrupt)

	go func() {
		// This will block until a signal is sent
		sig := <-signals

		Logger.Debugf("Received signal `%s`", sig.String())

		// If we've received a SIGKILL, die immediately.
		// if sig == syscall.SIGKILL {
		// 	Logger.Debugf("Exiting immediately", sig.String())

		// 	os.Exit(1)
		// }

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
				Logger.Warn("Waiting for job to finish before stopping. Send the signal again to exit immediately.")

				a.stopping = true
			}
		}

		// Start monitoring signals again
		a.MonitorSignals()
	}()
}
