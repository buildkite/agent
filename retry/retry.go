package retry

import (
	"errors"
	"fmt"
	"time"
)

type Stats struct {
	Attempt   int
	Config    *Config
	breakNext bool
}

type Config struct {
	Maximum  int
	Interval time.Duration
	Forever  bool
}

// A human readable representation often useful for debugging.
func (s *Stats) String() string {
	str := fmt.Sprintf("Attempt %d/", s.Attempt)

	if s.Config.Forever {
		str = str + "∞"
	} else {
		str = str + fmt.Sprintf("%d", s.Config.Maximum)
	}

	if s.Config.Interval > 0 {
		str = str + fmt.Sprintf(" Retrying in %s", s.Config.Interval)
	}

	return str
}

// Allow a retry loop to break out of itself
func (s *Stats) Break() {
	s.breakNext = true
}

func Do(callback func(*Stats) error, config *Config) error {
	var err error

	// Setup a default config for the retry
	if config == nil {
		config = &Config{Forever: true, Interval: 1 * time.Second}
	}

	// If the config isn't set to run forever, and the maximum is 0, set a
	// default of 0
	if config.Maximum == 0 && config.Forever == false {
		config.Maximum = 10
	}

	// Don't allow a forever retry without an interval
	if config.Forever && config.Interval == 0 {
		return errors.New("You can't do a forever retry with no interval")
	}

	// The stats struct that is passed to every attempt of the callback
	stats := &Stats{Attempt: 1, Config: config}

	for {
		// Attempt the callback
		err = callback(stats)
		if err == nil {
			return nil
		}

		// If the loop has callen stats.Break(), we should cancel out
		// of the loop
		if stats.breakNext {
			return err
		}

		time.Sleep(config.Interval)
		stats.Attempt = stats.Attempt + 1

		if !stats.Config.Forever {
			// Should we give up?
			if stats.Attempt > stats.Config.Maximum {
				break
			}
		}
	}

	return err
}
