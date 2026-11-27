package cliconfig

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v4/internal/osutil"
)

type File struct {
	// The path to the file
	Path string

	// A map of key/values that was loaded from the file
	Config map[string]string

	// LiteralKeys preserve JSON escapes rather than applying legacy unquoting.
	LiteralKeys []string
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
			if sep := strings.IndexAny(fullLine, "=:"); sep >= 0 {
				key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(fullLine[:sep]), "export"))
				if slices.Contains(f.LiteralKeys, key) {
					f.Config[key] = literalValue(fullLine[sep+1:])
					continue
				}
			}
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

// literalValue uses the JSON boundary only to remove an optional config quote
// wrapper and trailing comment. Validation is deferred to the consumer so a
// disabled setting can contain malformed JSON without changing legacy behaviour.
func literalValue(value string) string {
	value = strings.TrimSpace(value)
	text := value
	var quote byte
	if len(text) > 0 && (text[0] == '\'' || text[0] == '"') {
		quote, text = text[0], text[1:]
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err == nil {
		tail := strings.TrimSpace(text[decoder.InputOffset():])
		if quote != 0 && len(tail) > 0 && tail[0] == quote {
			tail = strings.TrimSpace(tail[1:])
		} else if quote != 0 {
			return value
		}
		if tail == "" || strings.HasPrefix(tail, "#") {
			return string(raw)
		}
	}
	return value
}

func isIgnoredLine(line string) bool {
	trimmedLine := strings.Trim(line, " \n\t")
	return len(trimmedLine) == 0 || strings.HasPrefix(trimmedLine, "#")
}
