package shell

import "time"

func Round(d time.Duration) time.Duration {
	return round(d)
}

// WithProcessWaitDelay is a test-only shell option that overrides the derived
// process.Config.WaitDelay bound, keeping leaked-pipe regression tests fast.
func WithProcessWaitDelay(d time.Duration) NewShellOpt {
	return func(s *Shell) { s.processWaitDelay = d }
}
