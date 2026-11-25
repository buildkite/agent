package cliconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiteralConfigPreservesJSONAndLegacyValues(t *testing.T) {
	t.Parallel()
	for _, wrapper := range []string{"", "'", `"`} {
		t.Run(wrapper, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.cfg")
			raw := `{"team":"a#b's\\path\n\"quoted\""}`
			data := "json=" + wrapper + raw + wrapper + " # a comment\nlegacy=\"line1\\nline2\"\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			file := File{Path: path, LiteralKeys: []string{"json"}}
			if err := file.Load(); err != nil {
				t.Fatal(err)
			}
			if got := file.Config["json"]; got != raw {
				t.Errorf("JSON=%q, want %q", got, raw)
			}
			if got := file.Config["legacy"]; got != "line1\nline2" {
				t.Errorf("legacy=%q, want expanded newline", got)
			}
		})
	}
}
