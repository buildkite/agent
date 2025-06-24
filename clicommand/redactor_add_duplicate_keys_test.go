package clicommand

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
)

func TestParseSecretsWithDuplicateKeys(t *testing.T) {
	// Test case demonstrating duplicate key behavior
	jsonInput := `{"password":"first_secret","password":"second_secret","password":"final_secret"}`

	l := logger.NewBuffer()
	cfg := RedactorAddConfig{Format: FormatStringJSON}
	reader := strings.NewReader(jsonInput)

	secrets, err := ParseSecrets(l, cfg, reader)
	if err != nil {
		t.Fatalf("ParseSecrets failed: %v", err)
	}

	// Only the last value should be present
	if len(secrets) != 1 {
		t.Errorf("Expected 1 secret, got %d", len(secrets))
	}

	if secrets[0] != "final_secret" {
		t.Errorf("Expected 'final_secret', got '%s'", secrets[0])
	}

	t.Logf("Input JSON: %s", jsonInput)
	t.Logf("Parsed secrets: %v", secrets)
	t.Logf("Only the last value for duplicate keys is kept")
}

func TestJSONDuplicateKeyBehavior(t *testing.T) {
	// This test demonstrates the underlying JSON behavior
	// that causes duplicate keys to be overwritten

	jsonWithDuplicates := `{"key":"value1","key":"value2","key":"value3"}`

	// Parse with Go's JSON decoder
	var result map[string]string
	err := json.NewDecoder(bytes.NewReader([]byte(jsonWithDuplicates))).Decode(&result)
	if err != nil {
		t.Fatalf("JSON decode failed: %v", err)
	}

	// Verify only one key-value pair exists
	if len(result) != 1 {
		t.Errorf("Expected 1 key-value pair, got %d", len(result))
	}

	// Verify the last value is kept
	if result["key"] != "value3" {
		t.Errorf("Expected 'value3', got '%s'", result["key"])
	}

	t.Logf("JSON with duplicate keys: %s", jsonWithDuplicates)
	t.Logf("Parsed result: %v", result)
	t.Logf("Only the last value for duplicate keys is preserved")
}

// Example demonstrating the duplicate key behavior
func Example_duplicateKeysInJSON() {
	// This example shows how to use the redactor with JSON that has duplicate keys
	
	// JSON with duplicate keys - only the last value will be redacted
	jsonInput := `{"password":"old_password","password":"new_password","api_key":"secret_key"}`

	// When this JSON is processed by the redactor:
	// - "old_password" is ignored (duplicate key overwritten)
	// - "new_password" is added to redactor (last value for "password")
	// - "secret_key" is added to redactor (unique key)

	// Usage example:
	// echo '{"password":"old_password","password":"new_password","api_key":"secret_key"}' | buildkite-agent redactor add --format json
	
	// Result: Only "new_password" and "secret_key" will be redacted from logs
	// "old_password" will NOT be redacted because it was overwritten by the duplicate key
} 