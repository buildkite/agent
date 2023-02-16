package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func createTestGitRespository() (*gitRepository, error) {
	repo, err := newGitRepository()
	if err != nil {
		return nil, fmt.Errorf("creating git repo: %w", err)
	}

	if err = repo.CreateBranch("main"); err != nil {
		return nil, fmt.Errorf("creating main branch: %w", err)
	}

	if err := os.WriteFile(filepath.Join(repo.Path, "test.txt"), []byte("This is a test"), 0600); err != nil {
		return nil, fmt.Errorf("writing test.txt on branch: master: %w", err)
	}

	if err = repo.Add("test.txt"); err != nil {
		return nil, fmt.Errorf("`git add`ing test.txt on branch: master : %w", err)
	}

	if err = repo.Commit("Initial Commit"); err != nil {
		return nil, fmt.Errorf("committing on branch: master: %w", err)
	}

	if err = repo.CreateBranch("update-test-txt"); err != nil {
		return nil, fmt.Errorf("creating update-test-txt branch: %w", err)
	}

	if err := os.WriteFile(filepath.Join(repo.Path, "test.txt"), []byte("This is a test pull request"), 0600); err != nil {
		return nil, fmt.Errorf("writing test.txt on branch update-test-text: %w", err)
	}

	if err = repo.Add("test.txt"); err != nil {
		return nil, fmt.Errorf("`git add`ing test.txt on branch update-test-txt: %w", err)
	}

	if err = repo.Commit("PR Commit"); err != nil {
		return nil, fmt.Errorf("creating PR commit: %w", err)
	}

	if _, err = repo.Execute("update-ref", "refs/pull/123/head", "HEAD"); err != nil {
		return nil, fmt.Errorf("updateing refs/pull/123/head to HEAD: %w", err)
	}

	if err = repo.CheckoutBranch("main"); err != nil {
		return nil, fmt.Errorf("checking out main: %w", err)
	}

	return repo, nil
}

type gitRepository struct {
	Path string
}

func newGitRepository() (*gitRepository, error) {
	tempDirRaw, err := os.MkdirTemp("", "git-repo")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	// io.TempDir on Windows tilde-shortens (8.3 style?) long filenames in the path.
	// This becomes a problem when that path is used for `git add`;
	// git believes the tilde-style path is outside the repo, but accepts the long-filename path.
	//
	// C:\Users\Administrator\AppData\Local\Temp\2\git-repo162512502>git add C:\Users\ADMINI~1\AppData\Local\Temp\2\git-repo162512502\.buildkite\hooks\pre-exit.bat
	// fatal: C:\Users\ADMINI~1\AppData\Local\Temp\2\git-repo162512502\.buildkite\hooks\pre-exit.bat: 'C:\Users\ADMINI~1\AppData\Local\Temp\2\git-repo162512502\.buildkite\hooks\pre-exit.bat' is outside repository
	//
	// C:\Users\Administrator\AppData\Local\Temp\2\git-repo162512502>git add C:\Users\Administrator\AppData\Local\Temp\2\git-repo162512502\.buildkite\hooks\pre-exit.bat
	// (ok)
	//
	// Some attempts to resolve the TempDir to a full file path:
	// filepath.Abs:          C:\Users\ADMINI~1\AppData\Local\Temp\2\git-repo275254366
	// filepath.Clean:        C:\Users\ADMINI~1\AppData\Local\Temp\2\git-repo275254366
	// filepath.EvalSymlinks: C:\Users\Administrator\AppData\Local\Temp\2\git-repo275254366
	//
	// EvalSymlinks seems best? Maybe there's a better way?
	tempDir, err := filepath.EvalSymlinks(tempDirRaw)
	if err != nil {
		return nil, fmt.Errorf("EvalSymlinks for temp dir: %w", err)
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

func (gr *gitRepository) Commit(message string, params ...any) error {
	if _, err := gr.Execute("commit", "-m", fmt.Sprintf(message, params...)); err != nil {
		return err
	}
	return nil
}

func (gr *gitRepository) CheckoutBranch(branch string) error {
	if _, err := gr.Execute("checkout", branch); err != nil {
		return err
	}
	return nil
}

func (gr *gitRepository) CreateBranch(branch string) error {
	if _, err := gr.Execute("checkout", "-b", branch); err != nil {
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
		return "", fmt.Errorf("finding git executable on path: %w", err)
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
		if out, err := gr.Execute(args...); err != nil {
			return fmt.Errorf("executing git %v: %s (%v)", args, out, err)
		}
	}
	return nil
}

func (gr *gitRepository) RevParse(rev string) (string, error) {
	return gr.Execute("rev-parse", rev)
}
