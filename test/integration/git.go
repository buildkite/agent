package integration

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

type GitRepository struct {
	Path string
}

func NewGitRepository() (*GitRepository, error) {
	tempDir, err := ioutil.TempDir("", "git-repo")
	if err != nil {
		return nil, fmt.Errorf("Error creating temp dir: %v", err)
	}

	gr := &GitRepository{Path: tempDir}
	gitErr := gr.ExecuteAll([][]string{
		{"init"},
		{"config", "user.email", "you@example.com"},
		{"config", "user.name", "Your Name"},
	})

	return gr, gitErr
}

func (gr *GitRepository) Commit(message string, path string, content string) error {
	if err := ioutil.WriteFile(filepath.Join(gr.Path, path), []byte(content), 0600); err != nil {
		return err
	}
	if _, err := gr.Execute("add", path); err != nil {
		return err
	}
	if _, err := gr.Execute("commit", "-m", message); err != nil {
		return err
	}
	return nil
}

func (gr *GitRepository) Close() error {
	return os.RemoveAll(gr.Path)
}

func (gr *GitRepository) Execute(args ...string) (string, error) {
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

func (gr *GitRepository) ExecuteAll(argsSlice [][]string) error {
	for _, args := range argsSlice {
		if _, err := gr.Execute(args...); err != nil {
			return err
		}
	}
	return nil
}
