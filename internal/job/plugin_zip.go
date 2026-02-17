package job

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent/plugin"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/roko"
)

const (
	maxZipExtractedSize = 100 * 1024 * 1024 // 100MB
)

// checkoutZipPlugin downloads and extracts a zip plugin to the plugins directory
func (e *Executor) checkoutZipPlugin(ctx context.Context, p *plugin.Plugin, checkout *pluginCheckout, pluginDirectory string) error {
	// Extract the SHA256 hash from the version fragment if present (format: sha256:abc123...)
	wantSHA256 := ""
	if strings.HasPrefix(p.Version, "sha256:") {
		wantSHA256 = strings.TrimPrefix(p.Version, "sha256:")
	}

	// Remove existing directory if present, as of right now caching is not supported.
	if osutil.FileExists(pluginDirectory) {
		if err := os.RemoveAll(pluginDirectory); err != nil {
			e.shell.Errorf("Failed to remove existing plugin directory %s", pluginDirectory)
			return err
		}
	}

	// Download and extract the plugin
	e.shell.Commentf("Plugin %q will be downloaded to %q", p.DisplayName(), pluginDirectory)

	// Construct the download URL
	downloadURL, err := constructZipPluginURL(p)
	if err != nil {
		return err
	}

	// Create temp directory for download
	tempDir, err := os.MkdirTemp(e.PluginsPath, "zip-plugin-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp dir

	zipPath := filepath.Join(tempDir, "plugin.zip")

	// Download the zip file
	gotSHA256, err := e.downloadZipPlugin(ctx, downloadURL, zipPath, wantSHA256)
	if err != nil {
		return fmt.Errorf("failed to download zip plugin: %w", err)
	}

	// Extract to temp directory first
	extractTempDir := filepath.Join(tempDir, "extract")
	if err := os.MkdirAll(extractTempDir, 0o777); err != nil {
		return fmt.Errorf("failed to create extract temp directory: %w", err)
	}

	if err := extractZipPlugin(zipPath, extractTempDir); err != nil {
		return fmt.Errorf("failed to extract zip plugin: %w", err)
	}

	// Move to final location
	e.shell.Commentf("Moving plugin to final location")
	if err := os.Rename(extractTempDir, pluginDirectory); err != nil {
		return fmt.Errorf("failed to move plugin to final location: %w", err)
	}

	e.shell.Commentf("Successfully downloaded and extracted plugin %q (SHA256: %s)", p.DisplayName(), gotSHA256)

	// Track the directory for cleanup at the end of the job
	e.cleanupDirs = append(e.cleanupDirs, pluginDirectory)

	// Open the plugin directory as the checkout root
	pluginRoot, err := os.OpenRoot(pluginDirectory)
	if err != nil {
		return fmt.Errorf("opening plugin directory as a root: %w", err)
	}
	runtime.AddCleanup(checkout, func(r *os.Root) { r.Close() }, pluginRoot)
	checkout.Root = pluginRoot

	// Ensure hooks is a directory that exists within the checkout
	if fi, err := pluginRoot.Stat(checkout.HooksDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%q was not a directory within the %q plugin: %w", checkout.HooksDir, checkout.Plugin.Name(), err)
	}

	return nil
}

// constructZipPluginURL builds the full URL for downloading the zip plugin
func constructZipPluginURL(p *plugin.Plugin) (string, error) {
	scheme := p.ZipBaseScheme()
	if scheme == "" {
		scheme = "https"
	}

	// Build the URL
	u := &url.URL{
		Scheme: scheme,
		Host:   "",
		Path:   p.Location,
	}

	// For file:// URLs, the location is the path
	if scheme == "file" {
		u.Path = p.Location
		return u.String(), nil
	}

	// For http/https URLs, split host and path
	parts := strings.SplitN(p.Location, "/", 2)
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid plugin location: %s", p.Location)
	}

	u.Host = parts[0]
	if len(parts) > 1 {
		u.Path = "/" + parts[1]
	}

	// Add authentication if present
	if p.Authentication != "" {
		userInfo := strings.Split(p.Authentication, ":")
		if len(userInfo) == 2 {
			u.User = url.UserPassword(userInfo[0], userInfo[1])
		} else {
			u.User = url.User(userInfo[0])
		}
	}

	return u.String(), nil
}

// downloadZipPlugin downloads a zip file from the given URL
func (e *Executor) downloadZipPlugin(ctx context.Context, downloadURL, destPath, wantSHA256 string) (string, error) {
	if e.Debug {
		e.shell.Commentf("Downloading from %s", downloadURL)
	}

	// Parse URL to get scheme
	u, err := url.Parse(downloadURL)
	if err != nil {
		return "", fmt.Errorf("invalid download URL: %w", err)
	}

	if u.Scheme == "file" {
		// Handle file:// URLs - copy local file
		if err := copyFile(u.Path, destPath); err != nil {
			return "", fmt.Errorf("failed to copy local file: %w", err)
		}
	} else if u.Scheme == "http" || u.Scheme == "https" {
		// For HTTP/HTTPS, download with retries
		err = roko.NewRetrier(
			roko.WithMaxAttempts(3),
			roko.WithStrategy(roko.Constant(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			return e.downloadZipPluginHTTP(ctx, downloadURL, destPath)
		})
		if err != nil {
			return "", err
		}
	} else {
		return "", fmt.Errorf("scheme %s is not supported", u.Scheme)
	}

	// Compute and verify SHA256
	gotSHA256, err := computeFileSHA256(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute SHA256: %w", err)
	}

	if wantSHA256 != "" && gotSHA256 != wantSHA256 {
		return "", fmt.Errorf("SHA256 verification failed: expected %s, got %s", wantSHA256, gotSHA256)
	}

	return gotSHA256, nil
}

// downloadZipPluginHTTP performs the actual HTTP download
func (e *Executor) downloadZipPluginHTTP(ctx context.Context, downloadURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", version.UserAgent())

	// Create HTTP client
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Create temp file
	tempFile, err := os.CreateTemp(filepath.Dir(destPath), "plugin-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Copy data to temp file
	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename to destination
	if err := os.Rename(tempFile.Name(), destPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst using io.Copy
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}

// computeFileSHA256 computes the SHA256 hash of a file
func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// extractZipPlugin extracts a zip file to the destination directory
func extractZipPlugin(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	// Check total size to prevent zip bombs
	var totalSize uint64
	for _, f := range r.File {
		totalSize += f.UncompressedSize64
		if totalSize > maxZipExtractedSize {
			return fmt.Errorf("zip archive too large (max %d MB)", maxZipExtractedSize/(1024*1024))
		}
	}

	// Extract files
	for _, f := range r.File {
		if err := extractZipFile(f, destPath); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile extracts a single file from a zip archive
func extractZipFile(f *zip.File, destPath string) error {
	// Validate path to prevent directory traversal
	cleanPath := filepath.Clean(filepath.Join(destPath, f.Name))
	relPath, err := filepath.Rel(destPath, cleanPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("invalid file path (possible traversal): %s", f.Name)
	}

	// Create directories if needed
	if f.FileInfo().IsDir() {
		return os.MkdirAll(cleanPath, 0o777)
	}

	// Reject symlinks for security
	if f.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not supported in zip plugins (found: %s)", f.Name)
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o777); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open zip file entry
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip entry: %w", err)
	}
	defer rc.Close()

	// Create destination file
	outFile, err := os.OpenFile(cleanPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	// Copy content
	if _, err := io.Copy(outFile, rc); err != nil {
		return fmt.Errorf("failed to extract file: %w", err)
	}

	return nil
}
