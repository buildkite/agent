package job

import (
	"regexp"
	"strings"
	"testing"
)

func TestValidateRepositoryAllowedForCheckout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		patterns   []string
		repository string
		wantErr    bool
	}{
		{name: "no allowlist", repository: "https://any.example/repo.git"},
		{name: "empty repository without allowlist"},
		{name: "matches second pattern", patterns: []string{`^git@github\.com:org/`, `^https://github\.com/org/`}, repository: "https://github.com/org/repo.git"},
		{name: "disallowed", patterns: []string{`^https://github\.com/org/`}, repository: "https://other.example/repo.git", wantErr: true},
		{name: "empty repository with allowlist", patterns: []string{`^https://github\.com/org/`}, wantErr: true},
		{name: "redacts rejected URL credentials", patterns: []string{`^https://github\.com/org/`}, repository: "https://user:secret@other.example/repo.git", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &Executor{}
			for _, pattern := range tc.patterns {
				e.AllowedRepositories = append(e.AllowedRepositories, regexp.MustCompile(pattern))
			}
			err := e.validateRepositoryAllowedForCheckout(tc.repository)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateRepositoryAllowedForCheckout() = %v, wantErr %t", err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Errorf("rejection leaked URL credentials: %v", err)
			}
		})
	}
}

func TestMatchesAllowedPatterns(t *testing.T) {
	t.Parallel()
	bkTarget := "github.com/buildkite/test"
	bkTargetRE := regexp.MustCompile(`^github\.com/buildkite/.*`)
	ghTargetRE := regexp.MustCompile(`^github\.com/nope/.*`)

	tests := []struct {
		name           string
		allowedTargets []*regexp.Regexp
		pipelineTarget string
		wantAllowed    bool
	}{
		{
			name:           "no allowlist",
			allowedTargets: []*regexp.Regexp{},
			pipelineTarget: bkTarget,
			wantAllowed:    true,
		}, {
			name:           "no match",
			allowedTargets: []*regexp.Regexp{ghTargetRE},
			pipelineTarget: bkTarget,
		}, {
			name:           "matches second pattern",
			allowedTargets: []*regexp.Regexp{ghTargetRE, bkTargetRE},
			pipelineTarget: bkTarget,
			wantAllowed:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchesAllowedPatterns(tc.allowedTargets, tc.pipelineTarget); got != tc.wantAllowed {
				t.Errorf("MatchesAllowedPatterns() = %t, want %t", got, tc.wantAllowed)
			}
		})
	}
}
