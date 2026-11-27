package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	s3TagCharacters    = regexp.MustCompile(`^[\p{L}\p{Z}\p{N}_.:/=+\-]*$`)
	azureTagCharacters = regexp.MustCompile(`^[a-zA-Z0-9 +\-./:=_]*$`)
)

func (c UploaderConfig) resolveObjectAttributes() (map[string]string, error) {
	if !c.ObjectAttributes {
		return nil, nil
	}

	var active string
	switch {
	case strings.HasPrefix(c.Destination, "s3://"):
		active = "s3-object-tags"
	case strings.HasPrefix(c.Destination, "gs://"):
		active = "gcs-object-contexts"
	case IsAzureBlobPath(c.Destination):
		active = "azure-blob-tags"
	}
	for _, flag := range c.ExplicitAttributeFlags {
		if flag != active {
			return nil, fmt.Errorf("--%s is not supported for this upload destination", flag)
		}
	}

	var selected map[string]string
	for _, setting := range []struct{ name, raw string }{
		{"s3-object-tags", c.S3ObjectTags},
		{"gcs-object-contexts", c.GCSObjectContexts},
		{"azure-blob-tags", c.AzureBlobTags},
	} {
		if active != "" && setting.name != active {
			continue
		}
		attributes, err := parseObjectAttributes(setting.raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", setting.name, err)
		}
		if active == "" {
			if len(attributes) > 0 {
				return nil, errors.New("object attributes require a private S3, GCS or Azure upload destination")
			}
			continue
		}
		if err := validateObjectAttributes(active, attributes); err != nil {
			return nil, fmt.Errorf("%s: %w", setting.name, err)
		}
		selected = attributes
	}
	return selected, nil
}

func parseObjectAttributes(raw string) (map[string]string, error) {
	// Decode via pointers: decoding JSON null straight into a string silently
	// accepts it as the empty string. Do not include customer values in errors.
	var values map[string]*string
	if !utf8.ValidString(raw) || json.Unmarshal([]byte(raw), &values) != nil || values == nil {
		return nil, errors.New("expected a JSON object with string values (use {} to clear attributes)")
	}
	attributes := make(map[string]string, len(values))
	for key, value := range values {
		if value == nil {
			return nil, errors.New("expected a JSON object with string values, not null")
		}
		attributes[key] = *value
	}
	return attributes, nil
}

func validateObjectAttributes(provider string, attributes map[string]string) error {
	limit := 10
	if provider == "gcs-object-contexts" {
		limit = 50
	}
	if len(attributes) > limit {
		return fmt.Errorf("at most %d attributes are allowed", limit)
	}
	var total int
	for key, value := range attributes {
		switch provider {
		case "s3-object-tags":
			if strings.HasPrefix(key, "aws:") {
				return errors.New("tag keys must not start with the reserved aws: prefix")
			}
			if len(utf16.Encode([]rune(key))) < 1 || len(utf16.Encode([]rune(key))) > 128 || len(utf16.Encode([]rune(value))) > 256 {
				return errors.New("tag keys must be 1–128 UTF-16 units and values 0–256 UTF-16 units")
			}
			if !s3TagCharacters.MatchString(key) || !s3TagCharacters.MatchString(value) {
				return errors.New("tags may contain Unicode letters, numbers, separators and _ . : / = + - only")
			}
		case "gcs-object-contexts":
			if strings.HasPrefix(key, "google:") {
				return errors.New("context keys must not start with the reserved google: prefix")
			}
			for _, text := range []string{key, value} {
				first, _ := utf8.DecodeRuneInString(text)
				if len(text) < 1 || len(text) > 256 || (!unicode.IsLetter(first) && !unicode.IsDigit(first)) || strings.ContainsAny(text, "'\"\\/") {
					return errors.New("context keys and values must be 1–256 UTF-8 bytes, start with an alphanumeric character and contain no quotes or slashes")
				}
			}
			total += len(key) + len(value)
		case "azure-blob-tags":
			if len(key) < 1 || len(key) > 128 || len(value) > 256 || !azureTagCharacters.MatchString(key) || !azureTagCharacters.MatchString(value) {
				return errors.New("tag keys must be 1–128 characters and values 0–256 characters, using ASCII letters, digits, spaces and + - . / : = _ only")
			}
		}
	}
	if provider == "gcs-object-contexts" && total > 25*1024 {
		return errors.New("combined context keys and values must not exceed 25 KiB")
	}
	return nil
}
