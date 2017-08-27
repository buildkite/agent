package bootstrap

import "testing"

func TestFindingSSHTools(t *testing.T) {
	d, err := findSSHToolsDir()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Found ssh tools at %s ", d)
}
