package cliconfig

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/buildkite/agent/v3/internal/osutil"
)

type File struct {
	// The path to the file
	Path string

	// A map of key/values that was loaded from the file
	Config map[string]string
}

func (f *File) Load() error {
	// Set the default config
	f.Config = map[string]string{}

	// Figure out the absolute path
	absolutePath, err := f.AbsolutePath()
	if err != nil {
		return fmt.Errorf("getting absolute path for %s: %w", f.Path, err)
	}

	// Open the file
	file, err := os.Open(absolutePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", f.Path, err)
	}

	// Make sure the config file is closed when this function finishes.
	defer file.Close() //nolint:errcheck // it's only open for reading

	// Get all the lines in the file
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Parse each line
	for lineNum, fullLine := range lines {
		if !isIgnoredLine(fullLine) {
			key, value, err := parseLine(fullLine)
			if err != nil {
				return fmt.Errorf("parsing config line %d: %w", lineNum+1, err)
			}

			f.Config[key] = value
		}
	}

	return nil
}

func (f File) AbsolutePath() (string, error) {
	return osutil.NormalizeFilePath(f.Path)
}

func (f File) Exists() bool {
	// If getting the absolute path fails, we can just assume it doesn't
	// exit...probably...
	absolutePath, err := f.AbsolutePath()
	if err != nil {
		return false
	}

	if _, err := os.Stat(absolutePath); err == nil {
		return true
	} else {
		return false
	}
}

// This file parsing code was copied from:
// https://github.com/joho/godotenv/blob/master/godotenv.go
//
// The project is released under an MIT License, which can be seen here:
// https://github.com/joho/godotenv/blob/master/LICENCE
func parseLine(line string) (key, value string, err error) {
	if len(line) == 0 {
		return "", "", errors.New("zero length string")
	}

	// ditch the comments (but keep quoted hashes)
	if strings.Contains(line, "#") {
		segmentsBetweenHashes := strings.Split(line, "#")
		quotesAreOpen := false
		segmentsToKeep := make([]string, 0)
		for _, segment := range segmentsBetweenHashes {
			if strings.Count(segment, "\"") == 1 || strings.Count(segment, "'") == 1 {
				if quotesAreOpen {
					quotesAreOpen = false
					segmentsToKeep = append(segmentsToKeep, segment)
				} else {
					quotesAreOpen = true
				}
			}

			if len(segmentsToKeep) == 0 || quotesAreOpen {
				segmentsToKeep = append(segmentsToKeep, segment)
			}
		}

		line = strings.Join(segmentsToKeep, "#")
	}

	// now split key from value
	splitString := strings.SplitN(line, "=", 2)

	if len(splitString) != 2 {
		// try yaml mode!
		splitString = strings.SplitN(line, ":", 2)
	}

	if len(splitString) != 2 {
		return "", "", fmt.Errorf("can't separate key from value in string %q, no valid separators (= or :) found", line)
	}

	// Parse the key
	key = splitString[0]
	key = strings.TrimPrefix(key, "export")
	key = strings.TrimSpace(key)

	// Parse the value
	value = splitString[1]
	// trim
	value = strings.TrimSpace(value)

	// check if we've got quoted values
	if strings.Count(value, "\"") == 2 || strings.Count(value, "'") == 2 {
		// pull the quotes off the edges
		value = strings.Trim(value, "\"'")

		// expand quotes
		value = strings.ReplaceAll(value, "\\\"", "\"")
		// expand newlines
		value = strings.ReplaceAll(value, "\\n", "\n")
	}

	return key, value, nil
}

func isIgnoredLine(line string) bool {
	trimmedLine := strings.Trim(line, " \n\t")
	return len(trimmedLine) == 0 || strings.HasPrefix(trimmedLine, "#")
}
