package signalwatcher

type Signal string

func (s Signal) String() string {
	return string(s)
}

const (
	QUIT = Signal("QUIT")
)
