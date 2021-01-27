package proxy

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:generate go-bindata -pkg proxy data/

func compile(dest string, src string, vars []string) error {
	args := []string{
		"build",
		"-o", dest,
	}

	if len(vars) > 0 {
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

	debugf("Compile %#v finished in %s", args, time.Now().Sub(t))
	return nil
}

func compileClient(dest string, vars []string) error {
	data, err := Asset("data/client.go")
	if err != nil {
		return err
	}

	dir, err := ioutil.TempDir("", "binproxy")
	if err != nil {
		return fmt.Errorf("Error creating temp dir: %v", err)
	}

	defer os.RemoveAll(dir)

	err = ioutil.WriteFile(filepath.Join(dir, "client.go"), data, 0500)
	if err != nil {
		return err
	}

	return compile(dest, filepath.Join(dir, "client.go"), vars)
}
