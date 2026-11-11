package job

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// MatchesAllowedPatterns permits any value when no patterns are configured.
func MatchesAllowedPatterns(patterns []*regexp.Regexp, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

// RepositoryAllowed rejects HTTP paths that can resolve to a different
// repository after the allowlist check. Keep matching the supplied URL so
// agent-managed Git credential rewrites of SSH URLs still work.
func RepositoryAllowed(patterns []*regexp.Regexp, repository string) bool {
	if len(patterns) == 0 {
		return true
	}
	scheme, _, hasScheme := strings.Cut(repository, ":")
	if hasScheme && (strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")) {
		u, err := url.Parse(repository)
		if err != nil {
			return false
		}
		// Path is percent-decoded, so this also catches encoded dot segments
		// and separators. A trailing slash on a repository URL is harmless.
		p := u.Path
		if len(p) > 1 && p[len(p)-2] != '/' {
			p = strings.TrimSuffix(p, "/")
		}
		if u.Host == "" || (p != "" && path.Clean(p) != p) {
			return false
		}
	}
	return MatchesAllowedPatterns(patterns, repository)
}
