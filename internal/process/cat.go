package process

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// Replicates how the command line tool `cat` works, but is more verbose about
// what it does
func Cat(path string) (string, error) {
	files, err := filepath.Glob(path)
	if err != nil {
		return "", fmt.Errorf("Failed to get a list of files: %w", err)
	}

	var buffer bytes.Buffer

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("Could not read file: %s (%T: %w)", file, err, err)
		}

		buffer.WriteString(string(data))
	}

	return buffer.String(), nil
}
