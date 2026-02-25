package experiments

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAvailableExperimentsDocumented(t *testing.T) {
	data, err := os.ReadFile("../../EXPERIMENTS.md")
	if err != nil {
		t.Fatalf("reading EXPERIMENTS.md: %v", err)
	}
	contents := string(data)

	for name := range Available {
		heading := fmt.Sprintf("### `%s`", name)
		if !strings.Contains(contents, heading) {
			t.Errorf("available experiment %q is missing a %q section in EXPERIMENTS.md", name, heading)
		}
	}
}
