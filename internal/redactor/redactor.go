// Package redactor provides an efficient configurable string redactor.
package redactor

import (
	"errors"
	"io"
	"path"
	"sync"

	"github.com/buildkite/agent/v3/bootstrap/shell"
)

// RedactLengthMin is the shortest string length that will be considered a
// potential secret by the environment redactor. e.g. if the redactor is
// configured to filter out environment variables matching *_TOKEN, and
// API_TOKEN is set to "none", this minimum length will prevent the word "none"
// from being redacted from useful log output.
const RedactLengthMin = 6

// Redactor is a straightforward secret redactor.
//
// The algorithm is intended to be easier to maintain than certain
// high-performance multi-string replacement algorithms, and also geared towards
// ensuring secrets don't escape (for instance, by matching overlaps), at the
// expense of ultimate efficiency.
type Redactor struct {
	// Replacement string (e.g. "[REDACTED]")
	subst []byte

	// Secrets to redact (looking for these needles in the haystack),
	// organised by first byte.
	// Why first byte? Because looking up needles by the first byte is a lot
	// faster than _filtering_ all the needles by first byte.
	needlesByFirstByte [256][]string

	// For synchronising writes. Each write can touch everything below.
	mu sync.Mutex

	// Redacted output written to this writer.
	dst io.Writer

	// Intermediate buffer to account for partially-written non-secrets.
	// (i.e. we began redacting in case we're in the middle of a secret, but
	// we might not be).
	buf []byte

	// Current redaction states - if we have begun redacting a potential secret
	// there will be at least one of these.
	// nextStates is the next set of states. To avoid creating millions of new
	// slices, Write alternates between these two.
	states, nextStates []state

	// The ranges in buf we must redact on flush.
	redact []subrange
}

// New returns a new Redactor.
func New(dst io.Writer, subst string, needles []string) *Redactor {
	r := &Redactor{
		dst:   dst,
		subst: []byte(subst),

		// Preallocate a few things.
		buf:        make([]byte, 0, 65536),
		states:     make([]state, 0, len(needles)),
		nextStates: make([]state, 0, len(needles)),
		redact:     make([]subrange, 0, len(needles)),
	}
	r.Reset(needles)
	return r
}

// Write redacts any secrets from the stream, and forwards the redacted stream
// to the destination writer.
func (r *Redactor) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// The high level:
	// 1. Append b to the buffer.
	// 2. Search through b to find instances of strings to redact. Store the
	//    ranges of redactions in r.redact.
	// 3. Merge overlapping redaction ranges.
	// 4. Write as much of the buffer as we can without spilling incomplete
	//    matches.
	//
	// Step 2 is complicated by the fact that each Write could contain a partial
	// secret at the start or the end. So a buffer is needed to hold onto any
	// incomplete matches (in case they _don't_ match), as well as some extra
	// state (r.states) for tracking where we are in each incomplete match.
	//
	// Step 4 (mostly in flushTo) only looks complicated because it has to
	// alternate between unredacted and redacted ranges, *and* handle the case
	// where we've chosen to flush to inside a redacted range.

	prevBufLen := len(r.buf)

	// 1. Append b to the buffer.
	r.buf = append(r.buf, b...)

	// 2. Search through b to find instances of strings to redact. Store the
	//    ranges of redactions in r.redact.
	for n, c := range b {
		bufidx := n + prevBufLen // where we are in the whole buffer

		// In the middle of redacting?
		for _, s := range r.states {
			// Does the needle match on this byte?
			if c != s.needle[s.matched] {
				continue
			}

			// It matched!
			s.matched++

			// Have we fully matched this needle?
			if s.matched < len(s.needle) {
				// This state survives for another byte.
				r.nextStates = append(r.nextStates, s)
				continue
			}

			// Match complete; save range to redact.
			r.redact = append(r.redact, subrange{
				from: bufidx - len(s.needle) + 1,
				to:   bufidx + 1,
			})
		}

		// Start redacting something?
		for _, s := range r.needlesByFirstByte[c] {
			if len(s) == 1 {
				// A pathological case; in practice we don't redact secrets
				// smaller than RedactLengthMin.
				r.redact = append(r.redact, subrange{
					from: bufidx,
					to:   bufidx + 1,
				})
				continue
			}
			r.nextStates = append(r.nextStates, state{
				needle:  s,
				matched: 1,
			})
		}

		// r.nextStates contains the new set of states.
		// Re-use the array underlying the old r.states for r.nextStates.
		r.states, r.nextStates = r.nextStates, r.states[:0]
	}

	// 3. Merge overlapping redaction ranges.
	// Because they were added from start to end, they are in order.
	r.redact = mergeOverlaps(r.redact)

	// 4. Write as much of the buffer as we can without spilling incomplete
	//    matches.
	flushTo := len(r.buf)
	for _, s := range r.states {
		if to := len(r.buf) - s.matched; to < flushTo {
			flushTo = to
		}
	}
	if err := r.flushTo(flushTo); err != nil {
		// We "wrote" this much of b in this Write at the point of error.
		return flushTo - prevBufLen, err
	}

	// We "wrote" all of b, so report len(b).
	return len(b), nil
}

// Flush writes all buffered data to the destination. It assumes there is no
// more data in the stream, and so any incomplete matches are non-matches.
func (r *Redactor) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// We know all the complete matches in each Write, what we don't know
	// are any incomplete matches. Since there is no more incoming data, any
	// remaining states must all be incomplete.
	r.states = r.states[:0]
	return r.flushTo(len(r.buf))
}

// flush writes out the buffer up to an index. flushTo is an upper limit.
func (r *Redactor) flushTo(flushTo int) error {
	if flushTo == 0 || len(r.buf) == 0 {
		return nil
	}

	bufidx := 0 // where we are up to in the buffer

	// Stop when we're out of redactions, or the next one is after flushTo.
	// Track the last range processed.
	done := -1
	for ri, rg := range r.redact {
		if rg.from >= flushTo {
			// This range is after the cutoff point.
			break
		}
		done = ri

		switch {
		case bufidx < rg.from:
			// A non-redacted range (followed by a redacted range).
			if _, err := r.dst.Write(r.buf[bufidx:rg.from]); err != nil {
				return err
			}
			fallthrough

		case bufidx == rg.from:
			// A redacted range.
			// Write a r.subst instead of the redacted range.
			if _, err := r.dst.Write(r.subst); err != nil {
				return err
			}
			bufidx = rg.to
			// bufidx could now be after flushTo, but that's OK.
			// We were going to write r.subst anyway. It just might be continued
			// by an overlap.

		default:
			// This should only happen if bufidx = 0 and a previous flush
			// moved earlier ranges before the start of the buffer.
			// r.subst should have been written in the earlier flush.
			bufidx = rg.to
		}
	}

	// Anything between here and flushTo?
	if bufidx < flushTo {
		if _, err := r.dst.Write(r.buf[bufidx:flushTo]); err != nil {
			return err
		}
		bufidx = flushTo
	}

	if bufidx >= len(r.buf) {
		// Truncate the buffer, preserving capacity.
		r.buf = r.buf[:0]

		// All the redactions were also processed.
		r.redact = r.redact[:0]
		return nil
	}

	// Keep the remainder of the buffer where it is. A future append might
	// create a new buffer, letting the old one be GC-ed.
	r.buf = r.buf[bufidx:]

	// Because redactions refer to buffer positions, and the buffer shrank,
	// update the redaction ranges to point at the correct locations in the
	// buffer. We also move them to the start of the slice to avoid allocation.
	rem := len(r.redact[done+1:]) // remaining ranges
	for i, rg := range r.redact[done+1:] {
		r.redact[i] = rg.sub(bufidx)
	}
	r.redact = r.redact[:rem]

	return nil
}

// Reset replaces the secrets to redact with a new set of secrets. It is not
// necessary to Flush beforehand, but:
//   - any previous secrets which have begun matching will continue matching
//     (until they reach a terminal state), and
//   - any new secrets will not be compared against existing buffer content,
//     only data passed to Write calls after Reset.
func (r *Redactor) Reset(needles []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.needlesByFirstByte {
		r.needlesByFirstByte[i] = nil
	}
	for _, s := range needles {
		if len(s) == 0 {
			continue
		}
		r.needlesByFirstByte[s[0]] = append(r.needlesByFirstByte[s[0]], s)
	}
}

// state tracks how far through one of the needles we are redacting.
type state struct {
	needle  string
	matched int
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

// ValuesToRedact returns the variable values to be redacted, given a
// redaction config string and an environment map.
func ValuesToRedact(logger shell.Logger, patterns []string, environment map[string]string) []string {
	vars := VarsToRedact(logger, patterns, environment)
	if len(vars) == 0 {
		return nil
	}

	vals := make([]string, 0, len(vars))
	for _, val := range vars {
		vals = append(vals, val)
	}

	return vals
}

// VarsToRedact returns the variable names and values to be redacted, given a
// redaction config string and an environment map.
func VarsToRedact(logger shell.Logger, patterns []string, environment map[string]string) map[string]string {
	// Lifted out of Bootstrap.setupRedactors to facilitate testing
	vars := make(map[string]string)

	for name, val := range environment {
		for _, pattern := range patterns {
			matched, err := path.Match(pattern, name)
			if err != nil {
				// path.ErrBadPattern is the only error returned by path.Match
				logger.Warningf("Bad redacted vars pattern: %s", pattern)
				continue
			}

			if !matched {
				continue
			}
			if len(val) < RedactLengthMin {
				if len(val) > 0 {
					logger.Warningf("Value of %s below minimum length (%d bytes) and will not be redacted", name, RedactLengthMin)
				}
				continue
			}

			vars[name] = val
			break // Break pattern loop, continue to next env var
		}
	}

	return vars
}

// Mux contains multiple redactors
type Mux []*Redactor

// Flush flushes all redactors.
func (mux Mux) Flush() error {
	var errs []error
	for _, r := range mux {
		if err := r.Flush(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Reset resets all redactors with new needles (secrets).
func (mux Mux) Reset(needles []string) {
	for _, r := range mux {
		r.Reset(needles)
	}
}
