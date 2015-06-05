package signalwatcher

import (
	"os"
	"os/signal"
)

func Watch(callback func(Signal)) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	go func() {
		<-signals

		go callback(QUIT)
		Watch(callback)
	}()
}
