// +build !windows

package stdin

import "os"

func IsPipe() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	} else {
		return (stat.Mode() & os.ModeCharDevice) == 0
	}
}
