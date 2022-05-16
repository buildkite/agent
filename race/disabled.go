//go:build !race

package race

// We can use this constant to determine whether or not the Go Race Detector is enabled. If the race detector is enabled,
// the `race` build tag is passed, so this file won't be compiled, where the other one (./enabled.go) will be.
// That is, if the race detector is enabled, the constant below will be false. Otherwise, it'll be true.
// It's kinda weird, but it's also how go itself does it: https://github.com/golang/go/blob/master/src/internal/race/race.go#L15
const Enabled = false
