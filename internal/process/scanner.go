package process

import (
	"bufio"
	"io"

	"github.com/buildkite/agent/v3/logger"
)

type Scanner struct {
	logger logger.Logger
}

func NewScanner(l logger.Logger) *Scanner {
	return &Scanner{
		logger: l,
	}
}

func (s *Scanner) ScanLines(r io.Reader, f func(line string)) error {
	reader := bufio.NewReader(r)
	var appending []byte

	s.logger.Debug("[LineScanner] Starting to read lines")

	// Note that we do this manually rather than
	// because we need to handle very long lines

	for {
		line, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				s.logger.Debug("[LineScanner] Encountered EOF")
				break
			}
			return err
		}

		// If isPrefix is true, that means we've got a really
		// long line incoming, and we'll keep appending to it
		// until isPrefix is false (which means the long line
		// has ended.
		if isPrefix && appending == nil {
			s.logger.Debug("[LineScanner] Line is too long to read, going to buffer it until it finishes")

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
				s.logger.Debug("[LineScanner] Finished buffering long line")
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

	s.logger.Debug("[LineScanner] Finished")
	return nil
}
