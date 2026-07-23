package archive

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestChecksumSHA256_Sum(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		input    []byte
	}{
		{
			name:     "empty input",
			input:    []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple string",
			input:    []byte("hello"),
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:     "binary data",
			input:    []byte{0xFF, 0x00, 0xAB, 0xCD},
			expected: "064145b73178d7c9fee36e70bb497d618fadb0e8a7f30b8fe7d9761ef1be635c",
		},
		{
			name:     "unicode string",
			input:    []byte("Hello 世界"),
			expected: "4487dd5e89032c1794903afe6f4b90aaab69972697ea5d3baa215df27c679803",
		},
		{
			name:     "long input",
			input:    bytes.Repeat([]byte("a"), 1000),
			expected: "41edece42d63e8d9bf515a9ba6932e1c20cbc9f5a5d134645adb5db1b9737ea3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChecksumSHA256{
				sha256: sha256.New(),
				f:      &bytes.Buffer{},
			}
			_, err := c.Write(tt.input)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			result := c.Sum()
			if result != tt.expected {
				t.Errorf("Sum() = %v, want %v", result, tt.expected)
			}
		})
	}
}
