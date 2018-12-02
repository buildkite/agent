package process

import (
	"bufio"
	"bytes"
	"io"
	"sync"

	"github.com/buildkite/agent/logger"
)

func ScanLines(r io.Reader, f func(line string)) error {
	var reader = bufio.NewReader(r)
	var appending []byte

	logger.Debug("[LineScanner] Starting to read lines")

	// Note that we do this manually rather than
	// because we need to handle very long lines

	for {
		line, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				logger.Debug("[LineScanner] Encountered EOF")
				break
			}
			return err
		}

		// If isPrefix is true, that means we've got a really
		// long line incoming, and we'll keep appending to it
		// until isPrefix is false (which means the long line
		// has ended.
		if isPrefix && appending == nil {
			logger.Debug("[LineScanner] Line is too long to read, going to buffer it until it finishes")

			// bufio.ReadLine returns a slice which is only valid until the next invocation
			// since it points to its own internal buffer array. To accumulate the entire
			// result we make a copy of the first prefix, and ensure there is spare capacity
			// for future appends to minimize the need for resizing on append.
			appending = make([]byte, len(line), (cap(line))*2)
			copy(appending, line)

			continue
		}

		// Should we be appending?
		if appending != nil {
			appending = append(appending, line...)

			// No more isPrefix! Line is finished!
			if !isPrefix {
				logger.Debug("[LineScanner] Finished buffering long line")
				line = appending

				// Reset appending back to nil
				appending = nil
			} else {
				continue
			}
		}

		// Write to the handler function
		f(string(line))
	}

	logger.Debug("[LineScanner] Finished")
	return nil
}

type LineBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

func (l *LineBuffer) WriteLine(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Finally write the line to the writer
	l.buf.Write([]byte(line + "\n"))
}

// Output returns the buffered output of the line processor
func (l *LineBuffer) Output() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.buf.String()
}
