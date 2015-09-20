package signalwatcher

type Signal string

func (s Signal) String() string {
	return string(s)
}

const (
	HUP  = Signal("HUP")
	QUIT = Signal("QUIT")
	TERM = Signal("TERM")
	INT  = Signal("INT")
)
