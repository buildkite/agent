package shell

// Logger represents a logger that outputs to a buildkite shell
type Logger interface {
	Printf(format string, v ...interface{})
	Headerf(format string, v ...interface{})
	Commentf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
	Warningf(format string, v ...interface{})
	Promptf(format string, v ...interface{})
}

var (
	_ Logger = &discardLogger{}
)

type discardLogger struct {
}

func (d *discardLogger) Printf(format string, v ...interface{})   {}
func (d *discardLogger) Headerf(format string, v ...interface{})  {}
func (d *discardLogger) Commentf(format string, v ...interface{}) {}
func (d *discardLogger) Errorf(format string, v ...interface{})   {}
func (d *discardLogger) Warningf(format string, v ...interface{}) {}
func (d *discardLogger) Promptf(format string, v ...interface{})  {}
