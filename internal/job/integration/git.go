package integration

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type gitRepository struct {
	Path string
}

var testGitRepositoryTemplate = sync.OnceValues(func() (string, error) {
	repo, err := createSeedTestGitRepository()
	if err != nil {
		return "", err
	}
	return repo.Path, nil
})

func createTestGitRespository() (*gitRepository, error) {
	templatePath, err := testGitRepositoryTemplate()
	if err != nil {
		return nil, err
	}

	repoPath, err := newGitRepositoryPath()
	if err != nil {
		return nil, err
	}
	if err := copyDirectory(templatePath, repoPath); err != nil {
		_ = os.RemoveAll(repoPath)
		return nil, fmt.Errorf("copying git repo template: %w", err)
	}

	return &gitRepository{Path: repoPath}, nil
}

func createSeedTestGitRepository() (*gitRepository, error) {
	repo, err := newGitRepository()
	if err != nil {
		return nil, err
	}

	if err = repo.CreateBranch("main"); err != nil {
		return nil, fmt.Errorf("creating main branch: %w", err)
	}

	if err := os.WriteFile(
		filepath.Join(repo.Path, "test.txt"),
		[]byte("This is a test"),
		0o600,
	); err != nil {
		return nil, fmt.Errorf("writing test.txt: %w", err)
	}

	if err = repo.Add("test.txt"); err != nil {
		return nil, fmt.Errorf("adding test.txt: %w", err)
	}

	if err = repo.Commit("Initial Commit"); err != nil {
		return nil, fmt.Errorf("initial commit: %w", err)
	}

	if err = repo.CreateBranch("update-test-txt"); err != nil {
		return nil, fmt.Errorf("creating branch: %w", err)
	}

	if err := os.WriteFile(
		filepath.Join(repo.Path, "test.txt"),
		[]byte("This is a test pull request"),
		0o600,
	); err != nil {
		return nil, fmt.Errorf("writing to test.txt: %w", err)
	}

	if err = repo.Add("test.txt"); err != nil {
		return nil, fmt.Errorf("adding test.txt again: %w", err)
	}

	if err = repo.Commit("PR Commit"); err != nil {
		return nil, fmt.Errorf("commit PR Commit: %w", err)
	}

	if _, err = repo.Execute("update-ref", "refs/pull/123/head", "HEAD"); err != nil {
		return nil, fmt.Errorf("updating ref: %w", err)
	}

	// Add the merge ref - this simulates what GitHub creates for PR merges
	if _, err := repo.Execute("merge-base", "main", "HEAD"); err != nil {
		return nil, fmt.Errorf("finding merge base: %w", err)
	}

	// Create a temporary merge commit for testing merge refspecs
	if err := repo.CheckoutBranch("main"); err != nil {
		return nil, fmt.Errorf("checkout main for merge: %w", err)
	}

	if _, err := repo.Execute("merge", "--no-ff", "-m", "Merge pull request #123", "update-test-txt"); err != nil {
		return nil, fmt.Errorf("creating merge commit: %w", err)
	}

	// Create the merge refspec that points to this merge commit
	if _, err := repo.Execute("update-ref", "refs/pull/123/merge", "HEAD"); err != nil {
		return nil, fmt.Errorf("updating merge ref: %w", err)
	}

	// Reset main back to its original state so unrelated tests aren't affected
	if _, err := repo.Execute("reset", "--hard", "HEAD~1"); err != nil {
		return nil, fmt.Errorf("resetting main: %w", err)
	}

	if err = repo.CheckoutBranch("main"); err != nil {
		return nil, fmt.Errorf("checkout main: %w", err)
	}

	return repo, nil
}

func newGitRepository() (*gitRepository, error) {
	tempDir, err := newGitRepositoryPath()
	if err != nil {
		return nil, err
	}

	gr := &gitRepository{Path: tempDir}
	gitErr := gr.ExecuteAll([][]string{
		{"init"},
		{"config", "user.email", "you@example.com"},
		{"config", "user.name", "Your Name"},
	})

	return gr, gitErr
}

func newGitRepositoryPath() (string, error) {
	tempDirRaw, err := os.MkdirTemp("", "git-repo")
	if err != nil {
		return "", fmt.Errorf("Error creating temp dir: %w", err)
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
		return "", fmt.Errorf("EvalSymlinks for temp dir: %w", err)
	}
	return tempDir, nil
}

func copyDirectory(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(dst, relPath)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		switch mode := info.Mode(); {
		case mode.IsDir():
			return os.MkdirAll(targetPath, mode.Perm())
		case mode.IsRegular():
			return copyFile(path, targetPath, mode)
		case mode&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		default:
			return fmt.Errorf("unsupported file mode %s for %s", mode, path)
		}
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	reader, err := os.Open(src)
	if err != nil {
		return err
	}
	defer reader.Close()

	writer, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(writer, reader); err != nil {
		writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode.Perm())
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
		return "", err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = gr.Path
	log.Printf("$ git %s", strings.Join(args, " "))
	out, err := cmd.CombinedOutput()
	log.Printf("Result: %v %s", err, out)
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

func (gr *gitRepository) RevParse(rev string) (string, error) {
	return gr.Execute("rev-parse", rev)
}
