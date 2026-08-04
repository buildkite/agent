package githttptest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Server struct {
	*httptest.Server
	repositories string
}

func NewServer() *Server {
	repositories, err := os.MkdirTemp("", "githttptest")
	if err != nil {
		panic(fmt.Sprintf("githttptest: failed to create temp dir: %v", err))
	}

	s := &Server{
		repositories: repositories,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/{repository}/git-upload-pack", s.handleGitUploadPack)
	mux.HandleFunc("/{repository}/git-receive-pack", s.handleGitReceivePack)
	mux.HandleFunc("/{repository}/info/refs", s.handleGitInfoRefs)

	s.Server = httptest.NewServer(mux)
	return s
}

func (s *Server) Close() {
	s.Server.Close()
	os.RemoveAll(s.repositories) //nolint:errcheck // Repos removal is best-effort
}

func (s *Server) RepoURL(repoName string) string {
	return fmt.Sprintf("%s/%s.git", s.URL, repoName)
}

func (s *Server) CreateRepository(repoName string) error {
	repoPath := filepath.Join(s.repositories, repoName)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		// Initialize a new bare Git repository
		if err := initBareRepo(repoPath); err != nil {
			return fmt.Errorf("failed to create repository: %w", err)
		}
	}

	return nil
}

func (s *Server) InitRepository(repoName string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "git-init-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck // Best-effort cleanup

	normalInitCmd := exec.Command("git", "init", tempDir)
	if out, err := normalInitCmd.CombinedOutput(); err != nil {
		return out, fmt.Errorf("failed to initialize normal repository: %w", err)
	}

	readmePath := filepath.Join(tempDir, "README.md")
	readmeContent := "# Git Repository\n\nThis repository was created by the Git HTTP server.\n"
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to create README file: %w", err)
	}

	addCmd := exec.Command("git", "add", "README.md")
	addCmd.Dir = tempDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return out, fmt.Errorf("failed to add README file: %w", err)
	}

	commitCmd := exec.Command("git", "commit", "-m", "Initial commit")
	commitCmd.Dir = tempDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return out, fmt.Errorf("failed to commit README file: %w", err)
	}

	url := fmt.Sprintf("%s/%s.git", s.URL, repoName)

	renameCmd := exec.Command("git", "branch", "-m", "main")
	renameCmd.Dir = tempDir
	if out, err := renameCmd.CombinedOutput(); err != nil {
		return out, fmt.Errorf("failed to rename branch to main: %w", err)
	}

	remoteAddCmd := exec.Command("git", "remote", "add", "origin", url)
	remoteAddCmd.Dir = tempDir
	if out, err := remoteAddCmd.CombinedOutput(); err != nil {
		return out, fmt.Errorf("failed to add remote origin: %w", err)
	}

	pushCmd := exec.Command("git", "push", "-u", "origin", "main")
	pushCmd.Dir = tempDir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return out, fmt.Errorf("failed to push to repository: %w", err)
	}

	return nil, nil
}

func (s *Server) PushBranch(repoName, branchName string) (string, []byte, error) {
	if branchName == "" || strings.ContainsAny(branchName, "\\/:*?\"<>|") {
		return "", nil, fmt.Errorf("invalid branch name: %s", branchName)
	}

	repoPath := filepath.Join(s.repositories, repoName)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("repository '%s' not found at path: %s", repoName, repoPath)
	}

	tempDir, err := os.MkdirTemp("", "git-push-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck // Best-effort cleanup

	cloneCmd := exec.Command("git", "clone", s.RepoURL(repoName), tempDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return "", out, fmt.Errorf("failed to clone repository: %w", err)
	}

	branchCmd := exec.Command("git", "checkout", "-b", branchName)
	branchCmd.Dir = tempDir
	if out, err := branchCmd.CombinedOutput(); err != nil {
		return "", out, fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	filePath := filepath.Join(tempDir, "newfile.txt")
	if err := os.WriteFile(filePath, []byte("This is a new file."), 0o644); err != nil {
		return "", nil, fmt.Errorf("failed to create new file: %w", err)
	}

	commitCmd := exec.Command("git", "add", "newfile.txt")
	commitCmd.Dir = tempDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return "", out, fmt.Errorf("failed to add new file to staging area: %w", err)
	}

	commitCmd = exec.Command("git", "commit", "-m", "Add new file")
	commitCmd.Dir = tempDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return "", out, fmt.Errorf("failed to commit new file: %w", err)
	}

	remoteCmd := exec.Command("git", "remote", "set-url", "origin", s.RepoURL(repoName))
	remoteCmd.Dir = tempDir
	if out, err := remoteCmd.CombinedOutput(); err != nil {
		return "", out, fmt.Errorf("failed to set remote URL: %w", err)
	}

	pushCmd := exec.Command("git", "push", "origin", branchName)
	pushCmd.Dir = tempDir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return "", out, fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	commitHashCmd := exec.Command("git", "rev-parse", branchName)
	commitHashCmd.Dir = tempDir
	commitHash, err := commitHashCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get commit hash: %w", err)
	}

	commitHashStr := strings.TrimSpace(string(commitHash))

	return commitHashStr, nil, nil
}

func (s *Server) CreateRef(repoName, refName, commitHash string) (out []byte, err error) {
	repoPath := filepath.Join(s.repositories, repoName)
	// Check if repository exists
	if _, err = os.Stat(repoPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("repository '%s' not found at path: %s", repoName, repoPath)
	}

	// git update-ref refs/heads/branch-name commit-sha
	updateRefCmd := exec.Command("git", "update-ref", refName, commitHash)
	updateRefCmd.Dir = repoPath
	if out, err = updateRefCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to create ref %s: %w", refName, err)
	}

	return out, nil
}

// isValidRepoName checks if the repository name is valid
func isValidRepoName(name string) bool {
	// Basic validation: non-empty, no slashes or special characters
	// Note: .git suffix should already be removed before this check
	return name != "" && !strings.ContainsAny(name, "\\/:*?\"<>|")
}

// initBareRepo initializes a new bare Git repository
func initBareRepo(path string) error {
	cmd := exec.Command("git", "init", "--bare", path)
	return cmd.Run()
}

// handleGitUploadPack handles the git-upload-pack endpoint (used for git fetch/clone)
func (s *Server) handleGitUploadPack(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repository")
	repoName = strings.TrimSuffix(repoName, ".git")

	if !isValidRepoName(repoName) {
		http.Error(w, "Invalid repository name", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.repositories, repoName)

	// Check if repository exists
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	buf := bytes.NewBuffer(nil)

	cmd := exec.Command("git", "upload-pack", "--stateless-rpc", repoPath)
	cmd.Stdin = r.Body
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, buf) //nolint:errcheck // The test should fail (incomplete response)
}

// handleGitReceivePack handles the git-receive-pack endpoint (used for git push)
func (s *Server) handleGitReceivePack(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repository")

	repoName = strings.TrimSuffix(repoName, ".git")

	if !isValidRepoName(repoName) {
		http.Error(w, "Invalid repository name", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.repositories, repoName)

	// Check if repository exists
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	buf := bytes.NewBuffer(nil)

	cmd := exec.Command("git", "receive-pack", "--stateless-rpc", repoPath)
	cmd.Stdin = r.Body
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, buf) //nolint:errcheck // The test should fail (incomplete response)
}

// handleGitInfoRefs handles the info/refs endpoint
func (s *Server) handleGitInfoRefs(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repository")

	repoName = strings.TrimSuffix(repoName, ".git")

	if !isValidRepoName(repoName) {
		http.Error(w, "Invalid repository name", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.repositories, repoName)

	service := r.URL.Query().Get("service")
	if service != "git-upload-pack" && service != "git-receive-pack" {
		http.Error(w, "Invalid service", http.StatusBadRequest)
		return
	}

	buf := bytes.NewBuffer(nil)

	// Execute the corresponding Git command
	cmd := exec.Command("git", strings.TrimPrefix(service, "git-"), "--stateless-rpc", "--advertise-refs", repoPath)
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// Smart HTTP protocol requires a specific preamble
	pktLine := fmt.Sprintf("# service=%s\n", service)
	fmt.Fprintf(w, "%04x%s0000", len(pktLine)+4, pktLine) //nolint:errcheck // The test should fail (incomplete response)
	io.Copy(w, buf)                                       //nolint:errcheck // The test should fail (incomplete response)
}
