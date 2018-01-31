package bintest

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	serverEnv = ``
	clientSrc = `package main

import (
	"github.com/buildkite/bintest"
	"os"
)

var (
	debug  string
	server string
)

func main() {
	c := bintest.NewClient(server)

	if debug == "true" {
		c.Debug = true
	}

	os.Exit(c.Run())
}
`
)

func compile(dest string, src string, vars []string) error {
	args := []string{
		"build",
		"-o", dest,
	}

	if len(vars) > 0 || Debug {
		args = append(args, "-ldflags")

		for idx, val := range vars {
			vars[idx] = "-X " + val
		}

		if Debug {
			vars = append(vars, "-X main.debug=true")
		}

		args = append(args, strings.Join(vars, " "))
	}

	t := time.Now()

	output, err := exec.Command("go", append(args, src)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Compile of %s failed: %s", src, output)
	}

	debugf("[compiler] Compiled %s in %v", dest, time.Now().Sub(t))
	return nil
}

func compileClient(dest string, vars []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	dir := fmt.Sprintf(`bintest_stub_%x`, md5.Sum([]byte(dest)))
	if err = os.Mkdir(filepath.Join(wd, dir), 0700); err != nil {
		return err
	}

	defer os.RemoveAll(dir)

	f := filepath.Join(dir, `main.go`)
	err = ioutil.WriteFile(f, []byte(clientSrc), 0500)
	if err != nil {
		return err
	}

	return compile(dest, f, vars)
}
