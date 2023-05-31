package mime

import (
	"strings"
	"testing"
)

func TestTypeByExtension(t *testing.T) {
	if len(types) == 0 {
		t.Error("Mime types list is empty")
	}
	for ext, expectedType := range types {
		if ext[0:1] != "." {
			t.Errorf(
				"File extension does not start with a leading dot: %s",
				ext,
			)
		}
		if len(strings.Split(expectedType, "/")) != 2 {
			t.Errorf(
				"Invalid mime type for %s: %s",
				ext,
				expectedType,
			)
		}
		t.Run(ext, func(t *testing.T) {
			resultType := TypeByExtension(ext)
			if resultType != expectedType {
				t.Errorf(
					"Unexpected mime type for %s: %s. Expected: %s",
					ext,
					resultType,
					expectedType,
				)
			}
		})
	}
}
