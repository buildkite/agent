package logger

type Level int

const (
	INFO Level = iota
	NOTICE
	DEBUG
	ERROR
	WARN
	FATAL
)

var levelNames = []string{
	"INFO",
	"NOTICE",
	"DEBUG",
	"ERROR",
	"WARN",
	"FATAL",
}

// String returns the string representation of a logging level.
func (p Level) String() string {
	return levelNames[p]
}
