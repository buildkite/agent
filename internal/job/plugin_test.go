package job

import (
	"strings"
	"testing"
)

func TestPluginCheckoutRevision(t *testing.T) {
	t.Parallel()

	sha1 := "0123456789abcdef0123456789abcdef01234567"
	sha256 := strings.Repeat("0123456789abcdef", 4)

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "full SHA-1", version: sha1, want: sha1 + "^{commit}"},
		{name: "full SHA-256", version: sha256, want: sha256 + "^{commit}"},
		{name: "uppercase full SHA-1", version: strings.ToUpper(sha1), want: strings.ToUpper(sha1) + "^{commit}"},
		{name: "39 hex chars", version: sha1[:39], want: sha1[:39]},
		{name: "41 hex chars", version: sha1 + "a", want: sha1 + "a"},
		{name: "short SHA", version: sha1[:7], want: sha1[:7]},
		{name: "40 chars with a non-hex char", version: "g" + sha1[1:], want: "g" + sha1[1:]},
		{name: "tag", version: "v1.2.3", want: "v1.2.3"},
		{name: "branch", version: "main", want: "main"},
		{name: "empty", version: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := pluginCheckoutRevision(test.version); got != test.want {
				t.Errorf("pluginCheckoutRevision(%q) = %q, want %q", test.version, got, test.want)
			}
		})
	}
}
