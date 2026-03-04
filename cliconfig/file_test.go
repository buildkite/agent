package cliconfig

import (
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{
			name:      "simple key value",
			line:      "token = abc123",
			wantKey:   "token",
			wantValue: "abc123",
		},
		{
			name:      "inline comment",
			line:      "token = abc123 # my token",
			wantKey:   "token",
			wantValue: "abc123",
		},
		{
			name:      "inline comment with apostrophe",
			line:      "token = abc123 # benno's token",
			wantKey:   "token",
			wantValue: "abc123",
		},
		{
			name:      "hash inside double quotes",
			line:      `name = "hello # world"`,
			wantKey:   "name",
			wantValue: "hello # world",
		},
		{
			name:      "hash inside single quotes",
			line:      `name = 'hello # world'`,
			wantKey:   "name",
			wantValue: "hello # world",
		},
		{
			name:      "hash inside quotes with trailing comment",
			line:      `name = "hello # world" # a comment`,
			wantKey:   "name",
			wantValue: "hello # world",
		},
		{
			name:    "empty line",
			line:    "",
			wantErr: true,
		},
		{
			name:    "no separator",
			line:    "just a string",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := parseLine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got key=%q value=%q", key, value)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if value != tt.wantValue {
				t.Errorf("value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}
