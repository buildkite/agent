package logger

type Level int

const (
	DEBUG Level = iota
	NOTICE
	INFO
	ERROR
	WARN
	FATAL
)

var levelNames = []string{
	"DEBUG",
	"NOTICE",
	"INFO",
	"ERROR",
	"WARN",
	"FATAL",
}

// String returns the string representation of a logging level.
func (p Level) String() string {
	return levelNames[p]
}
