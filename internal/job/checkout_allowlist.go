package job

import "regexp"

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
