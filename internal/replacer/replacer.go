// Package replacer provides an efficient configurable string replacer.
package replacer

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
)

// Replacer is a straightforward streaming string replacer suitable for
// detecting or redacting secrets in a stream.
//
// The algorithm is intended to be easier to maintain than certain
// high-performance multi-string search algorithms, and also geared towards
// ensuring strings don't escape (for instance, by matching overlaps), at the
// expense of ultimate efficiency.
type Replacer struct {
	// The replacement callback.
	replacement func([]byte) []byte

	// Strings to search for (looking for these needles in the haystack),
	// organised by first byte.
	// Why first byte? Because looking up needles by the first byte is a lot
	// faster than _filtering_ all the needles by first byte.
	needlesByFirstByte [256][]string

	// For synchronising writes. Each write can touch everything below.
	mu sync.Mutex

	// Output re-streamed to this writer.
	dst io.Writer

	// Intermediate buffer to account for partially-written data.
	buf []byte

	// Current redaction partialMatches - if we have begun matching a potential
	// needle there will be at least one of these.
	// nextMatches is the next set of partialMatches.
	// Write alternates between these two, rather than creating a new slice to
	// hold the next set of matches for every byte of the input.
	partialMatches, nextMatches []partialMatch

	// The ranges in buf we must replace on flush.
	completedMatches []subrange
}

// New returns a new Replacer.
//
// dst is the writer to which output is forwarded.
// needles is the list of strings to search for.
//
// replacement is called when one or more _overlapping_ needles are found.
// Non-overlapping matches (including adjacent matches) cause more callbacks.
// replacement is given the subslice of the internal buffer that matched one or
// more overlapping needles.
// The return value from replacement is used as a replacement for the range
// it was given. To forward the stream unaltered, simply return the argument.
// replacement can also scribble over the contents of the slice it gets (and
// return it), or return an entirely different slice of bytes to use for
// replacing the original in the forwarded stream.
// Because the callback semantics are "zero copy", replacement should _not_
// keep a reference to the argument after it returns, since that will prevent
// garbage-collecting old buffers. replacement should also avoid calling
// append on its input, or otherwise extend the slice, as this can overwrite
// more of the buffer than intended.
func New(dst io.Writer, needles []string, replacement func([]byte) []byte) *Replacer {
	r := &Replacer{
		replacement: replacement,
		dst:         dst,

		// Preallocate a few things.
		buf:              make([]byte, 0, 65536),
		partialMatches:   make([]partialMatch, 0, len(needles)),
		nextMatches:      make([]partialMatch, 0, len(needles)),
		completedMatches: make([]subrange, 0, len(needles)),
	}
	r.Reset(needles)
	return r
}

// Write searches the stream for needles (e.g. strings, secrets, ...), calls the
// replacement callback to obtain any replacements, and forwards the output to
// the destination writer.
func (r *Replacer) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// The high level:
	// 1. Append b to the buffer.
	// 2. Search through b to find needles. Store the ranges of complete matches
	//    in r.completedMatches.
	// 3. Merge overlapping ranges into one single range.
	// 4. Write as much of the buffer as we can without spilling incomplete
	//    matches.
	//
	// Step 2 is complicated by the fact that each Write could contain a partial
	// needle at the start or the end. So a buffer is needed to hold onto any
	// incomplete matches (in case they _don't_ match), as well as some extra
	// state (r.partialMatches) for tracking where we are in each incomplete
	// match.
	//
	// Step 4 (mostly in flushUpTo) only looks complicated because it has to
	// alternate between unmatched and matched ranges, *and* handle the case
	// where we've chosen to flush to inside a matched range.

	prevBufLen := len(r.buf)

	// 1. Append b to the buffer.
	r.buf = append(r.buf, b...)

	// 2. Search through b to find instances of strings to redact. Store the
	//    ranges of redactions in r.redact.
	for n, c := range b {
		bufidx := n + prevBufLen // where we are in the whole buffer

		// In the middle of matching?
		for _, s := range r.partialMatches {
			// It's one more byte from the stream that matched (if s survives
			// and is added to nextMatches).
			s.matched++

			// Does the needle match on this byte?
			switch s.needle[s.position] {
			case '\n':
				// Special-case \n since PTY cooked-mode turns \n into \r\n,
				// and `set -x` or even `echo` can turn it into ' ', and
				// multiline needles are normalised to a single \n where there
				// were multiple.

				switch c {
				case '\t', '\n', '\v', '\f', '\r', ' ':
					// Match repetitions of arbitrary whitespace.
					// State which continues matching \n:
					r.nextMatches = append(r.nextMatches, s)
					// State that has finished matching \n:
					s.position++ // and continue below

				default:
					continue
				}

			case c:
				// It matched!
				s.position++
				// continues below.

			default:
				// Did not match. Drop this partial match.
				continue
			}

			// Have we fully matched this needle?
			if s.position < len(s.needle) {
				// This state survives for another byte.
				r.nextMatches = append(r.nextMatches, s)
				continue
			}

			// Match complete; save range to redact.
			r.completedMatches = append(r.completedMatches, subrange{
				from: bufidx - s.matched + 1,
				to:   bufidx + 1,
			})
		}

		// Start matching something?
		for _, s := range r.needlesByFirstByte[c] {
			if len(s) == 1 {
				// A pathological case; in practice we don't redact secrets
				// smaller than RedactLengthMin.
				r.completedMatches = append(r.completedMatches, subrange{
					from: bufidx,
					to:   bufidx + 1,
				})
				continue
			}
			r.nextMatches = append(r.nextMatches, partialMatch{
				needle:   s,
				matched:  1,
				position: 1,
			})
		}

		// r.nextMatches now contains the new set of partial matches.
		// Re-use the storage for the old r.partialMatches for the new
		// r.nextMatches, instead of allocating a new one.
		r.partialMatches, r.nextMatches = r.nextMatches, r.partialMatches[:0]
	}

	// 3. Merge overlapping redaction ranges.
	// Because they were added from start to end, they are in order.
	r.completedMatches = mergeOverlaps(r.completedMatches)

	// 4. Write as much of the buffer as we can without spilling incomplete
	//    matches.
	limit := len(r.buf)
	for _, s := range r.partialMatches {
		if to := len(r.buf) - s.matched; to < limit {
			limit = to
		}
	}
	if err := r.flushUpTo(limit); err != nil {
		// We "wrote" this much of b in this Write at the point of error.
		return limit - prevBufLen, err
	}

	// We "wrote" all of b, so report len(b).
	return len(b), nil
}

// Flush writes all buffered data to the destination. It assumes there is no
// more data in the stream, and so any incomplete matches are non-matches.
func (r *Replacer) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Since there is no more incoming data, any remaining partial matches
	// cannot complete.
	r.partialMatches = r.partialMatches[:0]
	return r.flushUpTo(len(r.buf))
}

// flush writes out the buffer up to an index. limit is an upper limit.
func (r *Replacer) flushUpTo(limit int) error {
	if limit == 0 || len(r.buf) == 0 {
		return nil
	}

	bufidx := 0 // where we are up to in the buffer
	done := -1  // the index of the last match processed

	// Stop when we're out of completed matches, or the next one is after limit.

	for ri, match := range r.completedMatches {
		if match.from >= limit {
			// This range is after the cutoff point.
			break
		}

		if match.to > limit {
			// This range overlaps the cutoff point. Adjust the limit in order
			// to be able to give the complete start-to-end []byte of all
			// overlaps of the range to the callback (but in the next flush).
			limit = match.from
			break
		}

		if bufidx > match.from {
			// This should never happen. It would mean that we wrote some of
			// the buffer up to inside this match range. Maybe there's a bug
			// in mergeOverlaps?
			return fmt.Errorf("bufidx > match.from [%d > %d]", bufidx, match.from)
		}

		if bufidx < match.from {
			// This is the non-matching part of the buffer before this match.
			if _, err := r.dst.Write(r.buf[bufidx:match.from]); err != nil {
				return err
			}
		}

		// Now handle the match itself.
		// Call the replacement callback to get a replacement.
		if repl := r.replacement(r.buf[match.from:match.to]); len(repl) > 0 {
			if _, err := r.dst.Write(repl); err != nil {
				return err
			}
		}
		bufidx = match.to
		done = ri
	}

	// Anything non-matching between here and limit?
	if bufidx < limit {
		if _, err := r.dst.Write(r.buf[bufidx:limit]); err != nil {
			return err
		}
		bufidx = limit
	}

	// We got to the end of the buffer?
	if bufidx >= len(r.buf) {
		// Truncate the buffer, preserving capacity.
		r.buf = r.buf[:0]

		// All the redactions were also processed.
		r.completedMatches = r.completedMatches[:0]
		return nil
	}

	// Keep the remainder of the buffer where it is. A future append might
	// create a new buffer, letting the old one be GC-ed.
	r.buf = r.buf[bufidx:]

	// Because redactions refer to buffer positions, and the buffer shrank,
	// update the redaction ranges to point at the correct locations in the
	// buffer. We also move them to the start of the slice to avoid allocation.
	rem := len(r.completedMatches[done+1:]) // number of remaining matches
	for i, match := range r.completedMatches[done+1:] {
		// Note that i ranges from 0 to done, but `match` ranges the values:
		// r.completedMatches[0] = r.completedMatches[done+1].sub(bufidx)
		// r.completedMatches[1] = r.completedMatches[done+2].sub(bufidx)
		// etc
		r.completedMatches[i] = match.sub(bufidx)
	}
	r.completedMatches = r.completedMatches[:rem]

	return nil
}

// Size returns the number of needles
func (r *Replacer) Size() int {
	sum := 0
	for _, n := range r.needlesByFirstByte {
		sum += len(n)
	}
	return sum
}

// Needle returns the current needles
func (r *Replacer) Needles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	needles := make([]string, 0, r.Size())
	for _, m := range r.needlesByFirstByte {
		needles = append(needles, m...)
	}
	return needles
}

// Reset removes all current needes and sets new set of needles. It is not
// necessary to Flush beforehand, but:
//   - any previous needles which have begun matching will continue matching
//     (until they reach a terminal state), and
//   - any new needles will not be compared against existing buffer content,
//     only data passed to Write calls after Reset.
func (r *Replacer) Reset(needles []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.needlesByFirstByte {
		r.needlesByFirstByte[i] = nil
	}

	r.unsafeAdd(needles)
}

// Add adds more needles to be matched by the replacer. It is not necessary to
// Flush beforehand, but:
//   - any previous strings which have begun matching will continue matching
//     (until they reach a terminal state), and
//   - any new strings will not be compared against existing buffer content,
//     only data passed to Write calls after Add.
func (r *Replacer) Add(needles ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.unsafeAdd(needles)
}

func (r *Replacer) unsafeAdd(needles []string) {
	for _, s := range needles {
		s = NormaliseMultiline(s)
		if len(s) == 0 {
			continue
		}
		firstByte := s[0]
		// Check for the needle in the slice first (deduplication).
		// There aren't expected to be so many that this would be slow.
		if slices.Contains(r.needlesByFirstByte[firstByte], s) {
			continue
		}
		r.needlesByFirstByte[firstByte] = append(r.needlesByFirstByte[firstByte], s)
	}
}

func NormaliseMultiline(needle string) string {
	// Normalise multiline needles by splitting, trimming ' ' and \r from
	// each line, and reassembling.
	inLines := strings.Split(needle, "\n")
	outLines := make([]string, 0, len(inLines))
	for _, line := range inLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		outLines = append(outLines, trimmed)
	}
	return strings.Join(outLines, "\n")
}

// partialMatch tracks how far through one of the needles we have matched.
type partialMatch struct {
	needle   string
	matched  int // number of bytes i the stream matched
	position int // position within the needle matched up to
}

// subrange designates a contiguous range in a buffer (slice indexes: inclusive
// of from, exclusive of to).
type subrange struct {
	from, to int
}

func (r subrange) sub(x int) subrange {
	r.from -= x
	r.to -= x
	return r
}

// contains reports if the range contains x.
func (r subrange) contains(x int) bool {
	return r.from <= x && x < r.to
}

// overlap reports if the two ranges overlap in any way.
func (r subrange) overlap(s subrange) bool {
	return r.contains(s.from) || s.contains(r.from)
}

// union returns a range containing both r and s.
func (r subrange) union(s subrange) subrange {
	if r.from < s.from {
		s.from = r.from
	}
	if r.to > s.to {
		s.to = r.to
	}
	return s
}

// mergeOverlaps combines overlapping ranges. It alters the contents of the
// input, and assumes the ranges are sorted by "to".
func mergeOverlaps(rs []subrange) []subrange {
	// If there are none, or only one, then it's already merged.
	if len(rs) <= 1 {
		return rs
	}

	// Starting at the end and walking backwards to the start, consider merging
	// each rs[i] into rs[j].
	j := len(rs) - 1
	for i := j - 1; i >= 0; i-- {
		if rs[j].overlap(rs[i]) {
			rs[j] = rs[j].union(rs[i])
		} else {
			j--
			rs[j] = rs[i]
		}
	}

	// Move them to the start of the slice to avoid allocation.
	rem := len(rs[j:]) // # of remaining ranges
	copy(rs, rs[j:])
	return rs[:rem]
}
