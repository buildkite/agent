package integration

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var agentBinary string

// This init compiles a bootstrap to be invoked by the bootstrap tester
// We could possibly use the compiled test stub, but ran into some issues with mock compilation
func compileBootstrap(dir string) string {
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	binPath := filepath.Join(dir, "buildkite-agent")

	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, "main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = projectRoot

	err := cmd.Run()
	if err != nil {
		panic(err)
	}

	return binPath
}

func TestMain(m *testing.M) {
	dir, err := ioutil.TempDir("", "agent-binary")
	if err != nil {
		log.Fatal(err)
	}

	agentBinary = compileBootstrap(dir)
	code := m.Run()

	os.RemoveAll(dir)
	os.Exit(code)
}
