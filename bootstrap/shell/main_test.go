package shell_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/bintest"
)

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != `shell.test` {
		os.Exit(bintest.NewClientFromEnv().Run())
	}

	code := m.Run()
	os.Exit(code)
}
