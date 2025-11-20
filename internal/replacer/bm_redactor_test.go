package replacer_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

func BenchmarkBMRedactor(b *testing.B) {
	r := NewBMRedactor(io.Discard, "[REDACTED]", bigLipsumSecrets)
	for b.Loop() {
		if _, err := fmt.Fprintln(r, bigLipsum); err != nil {
			b.Errorf("fmt.Fprintln(r, bigLipsum) error = %v", err)
		}
	}
	if err := r.Flush(); err != nil {
		b.Errorf("r.Flush() = %v", err)
	}
}

// BoyerMooreRedactor is a redactor based on Boyer-Moore, and is kept around for
// benchmarking purposes.
type BoyerMooreRedactor struct {
	replacement []byte

	// Current offset from the start of the next input segment
	offset int

	// Minimum and maximum length of redactable string
	minlen int
	maxlen int

	// Table of Boyer-Moore skip distances, and values to redact matching this end byte
	table [256]struct {
		skip    int
		needles [][]byte
	}

	// Internal buffer for building redacted input into
	// Also holds the final portion of the previous Write call, in case of
	// sensitive values that cross Write boundaries
	outbuf []byte

	// Wrapped Writer that we'll send redacted output to
	output io.Writer
}

// Construct a new Redactor, and pre-compile the Boyer-Moore skip table
func NewBMRedactor(output io.Writer, replacement string, needles []string) *BoyerMooreRedactor {
	redactor := &BoyerMooreRedactor{
		replacement: []byte(replacement),
		output:      output,
	}
	redactor.Reset(needles)
	return redactor
}

// We re-use the same BoyerMooreRedactor between different hooks and the command
// We need to reset and update the list of needles between each phase
func (redactor *BoyerMooreRedactor) Reset(needles []string) {
	minNeedleLen := 0
	maxNeedleLen := 0
	for _, needle := range needles {
		if len(needle) < minNeedleLen || minNeedleLen == 0 {
			minNeedleLen = len(needle)
		}
		if len(needle) > maxNeedleLen {
			maxNeedleLen = len(needle)
		}
	}

	if redactor.outbuf == nil {
		// Linux pipes can buffer up to 65536 bytes before flushing, so there's
		// a reasonable chance that's how much we'll get in a single Write().
		// maxNeedleLen is added since we may retain that many bytes to handle
		// matches crossing Write boundaries.
		// It's a reasonable starting capacity which hopefully means we don't
		// have to reallocate the array, but append() will grow it if necessary
		redactor.outbuf = make([]byte, 0, 65536+maxNeedleLen)
	} else {
		redactor.outbuf = redactor.outbuf[:0]
	}

	// Since Boyer-Moore looks for the end of substrings, we can safely offset
	// processing by the length of the shortest string we're checking for
	// Since Boyer-Moore looks for the end of substrings, only bytes further
	// behind the iterator than the longest search string are guaranteed to not
	// be part of a match
	redactor.minlen = minNeedleLen
	redactor.maxlen = maxNeedleLen
	redactor.offset = minNeedleLen - 1

	// For bytes that don't appear in any of the substrings we're searching
	// for, it's safe to skip forward the length of the shortest search
	// string.
	// Start by setting this as a default for all bytes
	for i := range redactor.table {
		redactor.table[i].skip = minNeedleLen
		redactor.table[i].needles = nil
	}

	for _, needle := range needles {
		for i, ch := range []byte(needle) {
			// For bytes that do exist in search strings, find the shortest distance
			// between that byte appearing to the end of the same search string
			skip := len(needle) - i - 1
			if skip < redactor.table[ch].skip {
				redactor.table[ch].skip = skip
			}

			// Build a cache of which search substrings end in which bytes
			if skip == 0 {
				redactor.table[ch].needles = append(redactor.table[ch].needles, []byte(needle))
			}
		}
	}
}

func (redactor *BoyerMooreRedactor) Write(input []byte) (int, error) {
	// This is the no needles case, for example, Reset([]string{})
	if redactor.minlen == 0 && redactor.maxlen == 0 {
		return redactor.output.Write(input)
	}

	if len(input) == 0 {
		return 0, nil
	}

	// Current iterator index, how much we can safely consume from input without
	// reading past the end of any of the needle values.
	//
	// May be further than the number of bytes given in input.
	cursor := redactor.offset

	// Current index which is guaranteed to be completely redacted
	// May lag behind cursor by up to the length of the longest search string
	doneTo := 0

	// For so long as the safe consumption index is inside the current input array
	for cursor < len(input) {
		ch := input[cursor]
		skip := redactor.table[ch].skip

		possibleNeedleEnd := skip == 0

		// If the skip table tells us that there is no search string ending in
		// the current byte, skip forward by the indicated distance.
		if !possibleNeedleEnd {
			// Advance the safe to consume index, may be beyond the length of the current input
			cursor += skip

			// Also copy any content behind the cursor which is guaranteed not
			// to fall under a match.
			//
			// cursor is currently the most we could have safely read into input
			// (and having not found a needle there) plus the additional amount
			// we can now read before having to check for one
			//
			// cursor could now be pointing at the last byte of the maxlen needle
			// so the most bytes we can safely confirm are from up to that point.
			//
			// Everything in this input prior to the start of that needle can be
			// confirmed, and that needle (if present) will be redacted in the next
			// loop or next write.
			confirmedTo := min(cursor-redactor.maxlen, len(input))

			// Save the confirmed input to outbuf ready for pushing down and advance
			// doneTo to signal we have a confirmed series of bytes ready for pushing
			// down.
			if confirmedTo > doneTo {
				redactor.outbuf = append(redactor.outbuf, input[doneTo:confirmedTo]...)
				doneTo = confirmedTo
			}

			continue
		}

		// We'll check for matching search strings here, but we'll still need
		// to move the cursor forward
		// Since Go slice syntax is not inclusive of the end index, moving it
		// forward now reduces the need to use `cursor-1` everywhere
		cursor++
		for _, needle := range redactor.table[ch].needles {
			// Since we're working backwards from what may be the end of a
			// string, it's possible that the start would be out of bounds
			startSubstr := cursor - len(needle)
			var candidate []byte

			if startSubstr >= 0 {
				// If the candidate string falls entirely within input, then just slice into input
				candidate = input[startSubstr:cursor]
			} else if -startSubstr <= len(redactor.outbuf) {
				// If the candidate crosses the Write boundary, we need to
				// concatenate the two sections to compare against
				candidate = make([]byte, 0, len(needle))
				candidate = append(candidate, redactor.outbuf[len(redactor.outbuf)+startSubstr:]...)
				candidate = append(candidate, input[:cursor]...)
			} else {
				// Final case is that the start index is out of bounds, and
				// it's impossible for it to match. Just move on to the next
				// search substring
				continue
			}

			if bytes.Equal(needle, candidate) {
				if startSubstr < 0 {
					// If we accepted a negative startSubstr, the output buffer
					// needs to be truncated to remove the partial match
					redactor.outbuf = redactor.outbuf[:len(redactor.outbuf)+startSubstr]
				} else if startSubstr > doneTo {
					// First, copy over anything behind the matched substring unmodified
					redactor.outbuf = append(redactor.outbuf, input[doneTo:startSubstr]...)
				}
				// Then, write a fixed string into the output, and move doneTo past the redaction
				redactor.outbuf = append(redactor.outbuf, redactor.replacement...)
				doneTo = cursor

				// The next end-of-string will be at least this far away so
				// it's safe to skip forward a bit. May be beyond the current
				// input.
				cursor += redactor.minlen - 1
				break
			}
		}
	}

	// We buffer the end of the input in order to catch passwords that fall over Write boundaries.
	// In the case of line-buffered input, that means we would hold back the
	// end of the line in a user-visible way. For this reason, we push through
	// any line endings immediately rather than hold them back.
	// The \r case should help to handle progress bars/spinners that use \r to
	// overwrite the current line.
	// Technically this means that passwords containing newlines aren't
	// guarateed to get redacted, but who does that anyway?
	for i := doneTo; i < len(input); i++ {
		if input[i] == byte('\r') || input[i] == byte('\n') {
			redactor.outbuf = append(redactor.outbuf, input[doneTo:i+1]...)
			doneTo = i + 1
		}
	}

	var err error
	if doneTo > 0 {
		// Push the output buffer down
		_, err = redactor.output.Write(redactor.outbuf)

		// There will probably be a segment at the end of the input which may be a
		// partial match crossing the Write boundary. This is retained in the
		// output buffer to compare against on the next call
		// Flush() needs to be called after the final Write(), or this bit won't
		// get written
		redactor.outbuf = append(redactor.outbuf[:0], input[doneTo:]...)
	} else {
		// If nothing was done, just add what we got to the buffer to be
		// processed on the next run
		redactor.outbuf = append(redactor.outbuf, input...)
	}

	// We can offset the next Write processing by how far cursor is ahead of
	// the end of this input segment
	redactor.offset = cursor - len(input)

	return len(input), err
}

// Flush should be called after the final Write. This will Write() anything
// retained in case of a partial match and reset the output buffer.
func (redactor *BoyerMooreRedactor) Flush() error {
	_, err := redactor.output.Write(redactor.outbuf)
	redactor.outbuf = redactor.outbuf[:0]
	return err
}
