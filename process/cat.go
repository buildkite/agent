package process

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
)

// Replicates how the command line tool `cat` works, but is more verbose about
// what it does
func Cat(path string) (string, error) {
	files, err := filepath.Glob(path)
	if err != nil {
		return "", fmt.Errorf("Failed to get a list of files: %v", err)
	}

	var buffer bytes.Buffer

	for _, file := range files {
		data, err := ioutil.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("Could not read file: %s (%T: %v)", file, err, err)
		}

		buffer.WriteString(string(data))
	}

	return buffer.String(), nil
}
