package integration

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func createTestGitRespository() (*gitRepository, error) {
	repo, err := newGitRepository()
	if err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile(filepath.Join(repo.Path, "test.txt"), []byte("This is a test"), 0600); err != nil {
		return nil, err
	}

	if err = repo.Add("test.txt"); err != nil {
		return nil, err
	}

	if err = repo.Commit("Initial Commit"); err != nil {
		return nil, err
	}

	return repo, nil
}

type gitRepository struct {
	Path string
}

func newGitRepository() (*gitRepository, error) {
	tempDir, err := ioutil.TempDir("", "git-repo")
	if err != nil {
		return nil, fmt.Errorf("Error creating temp dir: %v", err)
	}

	gr := &gitRepository{Path: tempDir}
	gitErr := gr.ExecuteAll([][]string{
		{"init"},
		{"config", "user.email", "you@example.com"},
		{"config", "user.name", "Your Name"},
	})

	return gr, gitErr
}

func (gr *gitRepository) Add(path string) error {
	if _, err := gr.Execute("add", path); err != nil {
		return err
	}
	return nil
}

func (gr *gitRepository) Commit(message string, params ...interface{}) error {
	if _, err := gr.Execute("commit", "-m", fmt.Sprintf(message, params...)); err != nil {
		return err
	}
	return nil
}

func (gr *gitRepository) Close() error {
	return os.RemoveAll(gr.Path)
}

func (gr *gitRepository) Execute(args ...string) (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = gr.Path
	// log.Printf("$ git %v", args)
	out, err := cmd.CombinedOutput()
	// log.Printf("Result: %v %s", err, out)
	return string(out), err
}

func (gr *gitRepository) ExecuteAll(argsSlice [][]string) error {
	for _, args := range argsSlice {
		if _, err := gr.Execute(args...); err != nil {
			return err
		}
	}
	return nil
}
