// +build !windows

package signalwatcher

import (
	"os"
	"os/signal"
	"syscall"
)

func Watch(callback func(Signal)) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)

	go func() {
		sig := <-signals

		if sig == syscall.SIGHUP {
			go callback(HUP)
		} else {
			go callback(QUIT)
		}

		Watch(callback)
	}()
}
