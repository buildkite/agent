package process

import (
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Structure outputs messages written to it in a structured format, including metadata.
type Structure struct {
	w        io.Writer
	metadata map[string]string
	encoder  *json.Encoder
}

func NewStructure(w io.Writer, metadata map[string]string) *Structure {
	var metadata_copy = map[string]string{}
	for k, v := range metadata {
		metadata_copy[k] = v
	}
	return &Structure{w, metadata_copy, json.NewEncoder(w)}
}

// Write will write the given data out to the associated writer in a serialized JSON format, with fields including metadata and timestamp.
func (s *Structure) Write(data []byte) (n int, err error) {
	// When writing as a structured log, trailing newlines and carriage returns generally don't make sense.
	var msg = strings.TrimRight(string(data), "\r\n")
	var log = map[string]string{"message": msg, "timestamp": time.Now().UTC().Format(time.RFC3339)}
	for k, v := range s.metadata {
		log[k] = v
	}
	if err = s.encoder.Encode(log); err != nil {
		return 0, err
	}
	return len(data), nil
}
