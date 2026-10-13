// Package redact provides functions for determining values to redact.
package redact

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/replacer"
)

// LengthMin is the shortest string length that will be considered a
// potential secret by the environment redactor. e.g. if the redactor is
// configured to filter out environment variables matching *_TOKEN, and
// API_TOKEN is set to "none", this minimum length will prevent the word "none"
// from being redacted from useful log output.
const LengthMin = 6

// Redacted ignores its input and returns "[REDACTED]".
func Redacted([]byte) []byte { return []byte("[REDACTED]") }

// tokenPrefixes are the prefixes of tokens issued by Buildkite. Keep in sync
// with app/models/token_prefixes.rb in the Buildkite codebase.
var tokenPrefixes = []string{
	"bkaa_",  // agent access token
	"bkjat_", // agent job acquisition token
	"bkaj_",  // agent job token
	"bkar_",  // agent registration token
	"bkua_",  // API token
	"bkur_",  // OAuth refresh token
	"bktx_",  // token exchange
	"bkcqt_", // cluster queue token
	"bkct_",  // cluster token
	"bkpt_",  // packages temporary token; also deprecated portal token
	"bkrt_",  // packages registry token
	"bktr_",  // pipeline trigger token
	"bkat_",  // pipeline access token
	"bkpat_", // portal token
	"bkps_",  // portal secret
}

// tokenBody is the set of bytes that can appear in a Buildkite token after
// its prefix: the base64url alphabet, plus '.', which separates the parts of
// tokens that embed an organization ID or are JWTs.
const tokenBody = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_."

const (
	// TokenBodyLengthMin is the shortest token body (the part after the
	// prefix) that is redacted. The shortest tokens Buildkite issues have
	// bodies of 38 bytes or more; this is lower to also catch tokens that have
	// been truncated (e.g. by `ps` cutting off a long command line), while
	// leaving short placeholders like "bkjat_encoded-token" alone.
	TokenBodyLengthMin = 24

	// TokenBodyLengthMax is the longest token body that is redacted as one
	// match. It bounds how much output is held back while a possible token is
	// being matched. Job acquisition tokens are JWTs and the longest tokens
	// Buildkite issues, at several hundred bytes.
	TokenBodyLengthMax = 2048
)

// TokenPrefixes returns replacer prefixes that match anything that looks like
// a Buildkite-issued token: one of the known token prefixes followed by a
// body of base64url characters (plus '.') that is at least TokenBodyLengthMin
// long. Unlike needles, these match tokens whose values are not known in
// advance, such as the job acquisition token in the agent's command line.
func TokenPrefixes() []replacer.Prefix {
	prefixes := make([]replacer.Prefix, 0, len(tokenPrefixes))
	for _, p := range tokenPrefixes {
		prefixes = append(prefixes, replacer.Prefix{
			Prefix:  p,
			Body:    tokenBody,
			MinBody: TokenBodyLengthMin,
			MaxBody: TokenBodyLengthMax,
		})
	}
	return prefixes
}

// hasScheme matches URLs that begin with a "scheme://" prefix
var hasScheme = regexp.MustCompile(`^[^:]+://`)

// URLCredentials returns rawURL with all URL userinfo masked, since either
// the username or password may contain a token. URLs without userinfo are
// returned unchanged; an unparsable scheme-based URL returns a
// placeholder to avoid leaking a credential it may contain.
func URLCredentials(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		if hasScheme.MatchString(rawURL) {
			// Avoid returning the unparsed as it may contain an embedded
			// credential that we were unable to mask.
			return "(invalid URL)"
		}
		return rawURL
	}
	if u.User == nil {
		return rawURL
	}
	u.User = url.User("xxxxx")
	return u.String()
}

// String is a convenience wrapper for redacting small strings.
// This is fine to call repeatedly with many separate strings, but avoid using
// this to redact large streams - it requires buffering the whole input and
// output.
func String(input string, needles []string) string {
	var sb strings.Builder
	// strings.Builder.Write doesn't return an error, so neither should a
	// Replacer that writes to it. If there is a surprise error, that's panic
	// territory.
	repl := New(&sb, needles)
	if _, err := repl.Write([]byte(input)); err != nil {
		panic("Replacer failed to write to strings.Builder?")
	}
	if err := repl.Flush(); err != nil {
		panic("Replacer failed to flush to strings.Builder?")
	}
	return sb.String()
}

// NeedlesFromEnv matches the patterns against [os.Environ]. It returns values
// to redact and the names of env vars with "short" values.
func NeedlesFromEnv(patterns []string) (values, short []string, err error) {
	environ := env.FromSlice(os.Environ()).DumpPairs()
	toRedact, short, err := Vars(patterns, environ)
	if err != nil {
		return nil, nil, fmt.Errorf("finding env vars to redact: %w", err)
	}
	// Make the values unique.
	needles := make(map[string]struct{})
	for _, v := range toRedact {
		needles[v.Value] = struct{}{}
	}
	return slices.Collect(maps.Keys(needles)), short, nil
}

// Secrets is the set of values redacted from a job's output. Secrets are
// registered once, wherever they are discovered, and apply to every redactor
// created from the set, whether it was created before or after registration.
// Secrets are never removed, so a value stays redacted after the variable that
// held it changes. Secrets is safe for concurrent use.
type Secrets struct {
	mu      sync.Mutex
	values  map[string]struct{}
	writers []*replacer.Replacer
}

// NewSecrets returns an empty set of secrets.
func NewSecrets() *Secrets {
	return &Secrets{values: make(map[string]struct{})}
}

// Add registers values as secrets, along with their [GoEscaped] forms. Unlike
// [Secrets.AddVars], values of any length are registered.
func (s *Secrets) Add(values ...string) {
	values = AppendGoEscaped(values)

	s.mu.Lock()
	for _, v := range values {
		s.values[v] = struct{}{}
	}
	writers := slices.Clone(s.writers)
	s.mu.Unlock()

	// Writers created after the unlock already include the new values.
	for _, w := range writers {
		w.Add(values...)
	}
}

// AddVars registers the values of environment variables whose names match
// patterns. Values shorter than [LengthMin] are not registered; their names are
// returned in short, sorted, so the caller can warn about them.
func (s *Secrets) AddVars(patterns []string, environ []env.Pair) (short []string, err error) {
	matched, short, err := Vars(patterns, environ)
	values := make([]string, 0, len(matched))
	for _, pair := range matched {
		values = append(values, pair.Value)
	}
	s.Add(values...)
	slices.Sort(short)
	return short, err
}

// NewWriter returns a redactor that writes to dst. It redacts every secret,
// including those added later, and anything that looks like a
// Buildkite-issued token. The caller must flush it, directly or with
// [Secrets.Flush], to release withheld output.
func (s *Secrets) NewWriter(dst io.Writer) *replacer.Replacer {
	s.mu.Lock()
	defer s.mu.Unlock()

	w := s.newReplacer(dst)
	s.writers = append(s.writers, w)
	return w
}

// Snapshot returns a redactor like [Secrets.NewWriter], except that it only
// redacts the secrets registered so far, and is independent of the set:
// [Secrets.Flush] does not flush it, so it can redact a separate value without
// releasing or mixing with output withheld by other writers.
func (s *Secrets) Snapshot(dst io.Writer) *replacer.Replacer {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.newReplacer(dst)
}

func (s *Secrets) newReplacer(dst io.Writer) *replacer.Replacer {
	r := replacer.New(dst, slices.Collect(maps.Keys(s.values)), Redacted)
	// As well as known values, redact anything that looks like a
	// Buildkite-issued token. Some tokens are never registered (e.g. the job
	// acquisition token, which is only in the agent's command line) but can
	// still end up in output, e.g. via `ps`.
	r.AddPrefixes(TokenPrefixes()...)
	return r
}

// Flush flushes every writer created by [Secrets.NewWriter].
func (s *Secrets) Flush() error {
	s.mu.Lock()
	writers := slices.Clone(s.writers)
	s.mu.Unlock()

	errs := make([]error, 0, len(writers))
	for _, w := range writers {
		errs = append(errs, w.Flush())
	}
	return errors.Join(errs...)
}

// New returns a replacer configured to write to dst, and redact all needles.
func New(dst io.Writer, needles []string) *replacer.Replacer {
	return replacer.New(dst, AppendGoEscaped(needles), Redacted)
}

// GoEscaped is like [strconv.Quote], but without the surrounding double quotes.
func GoEscaped(s string) string {
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}

// AppendGoEscaped appends the [GoEscaped] versions of needles, as needed, to
// catch values printed via %q.
func AppendGoEscaped(needles []string) []string {
	for _, n := range needles {
		if n2 := GoEscaped(n); n != n2 {
			needles = append(needles, n2)
		}
	}
	return needles
}

// MatchAny reports if the name matches any of the patterns.
func MatchAny(patterns []string, name string) (matched bool, err error) {
	// Track patterns that couldn't be parsed by path.Match, and report them
	// in a single error.
	var badPatterns []string
	defer func() {
		if len(badPatterns) > 0 {
			slices.Sort(badPatterns)
			err = fmt.Errorf("bad patterns: %q", badPatterns)
		}
	}()

	for _, pattern := range patterns {
		matched, err := path.Match(pattern, name)
		if err != nil {
			badPatterns = append(badPatterns, pattern)
			continue
		}

		if matched {
			return true, nil
		}
	}
	return false, nil
}

// Vars returns the variable names and values to be redacted, given a
// redaction config string and an environment map. It also returned variables
// whose names match the redacted-vars config, but whose values were too short.
func Vars(patterns []string, environment []env.Pair) (matched []env.Pair, short []string, err error) {
	for _, pair := range environment {
		// Does the name match any of the patterns?
		m, err := MatchAny(patterns, pair.Name)
		if err != nil {
			return nil, nil, err
		}
		if !m {
			continue
		}

		// The name matched, now test the length of the value.
		if len(pair.Value) < LengthMin {
			if len(pair.Value) > 0 {
				short = append(short, pair.Name)
			}
			continue
		}

		matched = append(matched, pair)
	}

	return matched, short, nil
}
