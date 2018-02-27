package bintest

import "log"

var (
	Debug bool
)

func debugf(pattern string, args ...interface{}) {
	if Debug {
		log.Printf(pattern, args...)
	}
}
