package job

import (
	"testing"
)

func TestParseGitVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		output string
		want   gitVer
		wantOK bool
	}{
		{output: "git version 2.39.5", want: gitVer{2, 39}, wantOK: true},
		{output: "git version 2.26.0.windows.1", want: gitVer{2, 26}, wantOK: true},
		{output: "git version 3.0.0", want: gitVer{3, 0}, wantOK: true},
		{output: "not git", wantOK: false},
		{output: "git version", wantOK: false},
		{output: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			t.Parallel()
			got, gotOK := parseGitVersion(tt.output)
			if got != tt.want || gotOK != tt.wantOK {
				t.Errorf("parseGitVersion(%q) = (%v, %t), want (%v, %t)", tt.output, got, gotOK, tt.want, tt.wantOK)
			}
		})
	}
}

func TestGitVerAtLeast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		v, min gitVer
		want   bool
	}{
		{v: gitVer{2, 35}, min: gitVer{2, 35}, want: true},
		{v: gitVer{2, 43}, min: gitVer{2, 35}, want: true},
		{v: gitVer{2, 34}, min: gitVer{2, 35}, want: false},
		// A minor greater than the requirement must not rescue a lower major,
		// and vice versa.
		{v: gitVer{1, 99}, min: gitVer{2, 27}, want: false},
		{v: gitVer{3, 0}, min: gitVer{2, 35}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.v.String()+"_min_"+tt.min.String(), func(t *testing.T) {
			t.Parallel()
			if got := tt.v.atLeast(tt.min); got != tt.want {
				t.Errorf("gitVer{%s}.atLeast(%s) = %t, want %t", tt.v, tt.min, got, tt.want)
			}
		})
	}
}
