package bintest

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	deadlock "github.com/sasha-s/go-deadlock"
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

var (
	compileCacheInstance *compileCache
	compileLock          deadlock.Mutex
)

func compile(dest string, src string, vars []string) error {
	args := []string{
		"build",
		"-o", dest,
	}

	if len(vars) > 0 || Debug {
		varsCopy := make([]string, len(vars))
		copy(varsCopy, vars)

		args = append(args, "-ldflags")

		for idx, val := range varsCopy {
			varsCopy[idx] = "-X " + val
		}

		if Debug {
			varsCopy = append(varsCopy, "-X main.debug=true")
		}

		args = append(args, strings.Join(varsCopy, " "))
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
	serverLock.Lock()
	defer serverLock.Unlock()

	// first off we create a temp dir for caching
	if compileCacheInstance == nil {
		var err error
		compileCacheInstance, err = newCompileCache()
		if err != nil {
			return err
		}
	}

	// if we can, use the compile cache
	if compileCacheInstance.IsCached(vars) {
		return compileCacheInstance.Copy(dest, vars)
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	// we create a temp subdir relative to current dir so that
	// we can make use of gopath / vendor dirs
	dir := fmt.Sprintf(`_bintest_%x`, sha1.Sum([]byte(clientSrc)))
	f := filepath.Join(dir, `main.go`)

	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		_ = os.Mkdir(filepath.Join(wd, dir), 0700)

		if err = ioutil.WriteFile(f, []byte(clientSrc), 0500); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
	}

	if err := compile(dest, f, vars); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}

	// cache for next time
	if err := compileCacheInstance.Cache(dest, vars); err != nil {
		return err
	}

	return os.RemoveAll(dir)
}

type compileCache struct {
	Dir string
}

func newCompileCache() (*compileCache, error) {
	cc := &compileCache{}

	var err error
	cc.Dir, err = ioutil.TempDir("", "binproxy")
	if err != nil {
		return nil, fmt.Errorf("Error creating temp dir: %v", err)
	}

	return cc, nil
}

func (c *compileCache) IsCached(vars []string) bool {
	path, err := c.file(vars)
	if err != nil {
		panic(err)
	}

	if _, err := os.Stat(path); err == nil {
		return true
	}

	return false
}

func (c *compileCache) Copy(dest string, vars []string) error {
	src, err := c.file(vars)
	if err != nil {
		return err
	}
	return copyFile(dest, src, 0777)
}

func (c *compileCache) Cache(src string, vars []string) error {
	dest, err := c.file(vars)
	if err != nil {
		return err
	}
	return copyFile(dest, src, 0666)
}

func (c *compileCache) Key(vars []string) (string, error) {
	h := sha1.New()

	// add the vars to the hash
	for _, v := range vars {
		if _, err := io.WriteString(h, v); err != nil {
			return "", err
		}
	}
	// factor in client source as well
	_, _ = io.WriteString(h, clientSrc)

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (c *compileCache) file(vars []string) (string, error) {
	if c.Dir == "" {
		return "", errors.New("No compile cache dir set")
	}

	k, err := c.Key(vars)
	if err != nil {
		return "", err
	}

	return filepath.Join(c.Dir, k), nil
}

func copyFile(dst, src string, perm os.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		err = in.Close()
	}()

	tmp, err := ioutil.TempFile(filepath.Dir(dst), "")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, in)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err = os.Chmod(tmp.Name(), perm); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}

	return os.Rename(tmp.Name(), dst)
}
