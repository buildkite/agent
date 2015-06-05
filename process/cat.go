package process

import (
	"bytes"
	"github.com/buildkite/agent/logger"
	"io/ioutil"
	"path/filepath"
)

// Replicates how the command line tool `cat` works, but is more verbose about
// what it does
func Cat(path string) string {
	files, err := filepath.Glob(path)

	if err != nil {
		logger.Debug("Failed to get list of files: %s", path)

		return ""
	} else {
		var buffer bytes.Buffer

		for _, file := range files {
			data, err := ioutil.ReadFile(file)

			if err != nil {
				logger.Debug("Could not read file: %s (%T: %v)", file, err, err)
			} else {
				buffer.WriteString(string(data))
			}
		}

		return buffer.String()
	}
}
