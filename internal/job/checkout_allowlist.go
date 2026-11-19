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

// RepositoryAllowed rejects paths that can resolve to a different repository
// after the allowlist check. Match the supplied URL so agent-managed Git
// credential rewrites of SSH URLs still work with the operator's patterns.
func RepositoryAllowed(patterns []*regexp.Regexp, repository string) bool {
	if len(patterns) == 0 {
		return true
	}
	urlToCheck := repository
	if strings.HasPrefix(repository, "git@github.com:") {
		// Managed Git credentials rewrite this SSH form to HTTPS before fetch.
		urlToCheck = "https://github.com/" + strings.TrimPrefix(repository, "git@github.com:")
	}
	scheme, _, hasScheme := strings.Cut(urlToCheck, ":")
	if hasScheme && (strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")) {
		u, err := url.Parse(urlToCheck)
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
