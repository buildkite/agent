package pipeline

import "testing"

func TestPluginFullSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source, want string
	}{
		{
			source: "thing",
			want:   "github.com/buildkite-plugins/thing-buildkite-plugin",
		},
		{
			source: "thing#main",
			want:   "github.com/buildkite-plugins/thing-buildkite-plugin#main",
		},
		{
			source: "my-org/thing",
			want:   "github.com/my-org/thing-buildkite-plugin",
		},
		{
			source: "./.buildkite/plugins/llamas/rock",
			want:   "./.buildkite/plugins/llamas/rock",
		},
		{
			source: `.\.buildkite\plugins\llamas\rock`,
			want:   `.\.buildkite\plugins\llamas\rock`,
		},
		{
			source: `C:\llamas\rock`,
			want:   `C:\llamas\rock`,
		},
		{
			source: `\\\\?\C:\user\docs`,
			want:   `\\\\?\C:\user\docs`,
		},
		{
			source: "/a-plugin",
			want:   "/a-plugin",
		},
		{
			source: "/my-org/a-plugin",
			want:   "/my-org/a-plugin",
		},
		{
			source: "https://my-plugin.git",
			want:   "https://my-plugin.git",
		},
		{
			source: "file:///Users/user/Desktop/my-plugin.git",
			want:   "file:///Users/user/Desktop/my-plugin.git",
		},
		{
			source: "git@github.com:buildkite/private-buildkite-plugin.git",
			want:   "git@github.com:buildkite/private-buildkite-plugin.git",
		},
		{
			source: "ssh://git@github.com:buildkite/private-buildkite-plugin.git",
			want:   "ssh://git@github.com:buildkite/private-buildkite-plugin.git",
		},
		{
			source: "my:plugin",
			want:   "my:plugin",
		},
	}

	for _, test := range tests {
		p := Plugin{
			Source: test.source,
		}
		if got, want := p.FullSource(), test.want; got != want {
			t.Errorf("%#v.FullSource() = %q, want %q", p, got, want)
		}

		// Test idempotency - the backend should be applying the same transform,
		// so it's important for multiple normalisations to be idempotent.
		p.Source = test.want
		if got, want := p.FullSource(), test.want; got != want {
			t.Errorf("%#v.FullSource() = %q, want %q", p, got, want)
		}
	}
}
