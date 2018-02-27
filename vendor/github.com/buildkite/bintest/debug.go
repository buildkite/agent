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

func errorf(pattern string, args ...interface{}) {
	log.Printf("\x1b[31;1m🚨 ERROR: "+pattern+"\x1b[0m", args...)
}
