package dockerbootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAuthenticationIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions and shell fixture")
	}
	dir := t.TempDir()
	encoded := base64.StdEncoding.EncodeToString([]byte("tester:synthetic-password"))
	config := `{"auths":{"registry.test":{"auth":"` + encoded + `"}},"credsStore":"test","proxies":{"default":{"httpProxy":"secret-proxy"}},"HttpHeaders":{"secret":"header"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(dir, "helpers.json")
	if err := os.WriteFile(envFile, []byte(`{"HELPER_SECRET":"synthetic-helper-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := LoadAuthentication(dir, envFile, "/usr/bin:/bin")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(auth.Config), "proxies") || strings.Contains(string(auth.Config), "HttpHeaders") {
		t.Fatal("non-authentication settings copied")
	}
	path := filepath.Join(t.TempDir(), "docker")
	script := `#!/bin/sh
printf '%s\n' "$@"
printf 'HELPER:%s\n' "${HELPER_SECRET-unset}"
printf 'JOB:%s\n' "${JOB_SECRET-unset}"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HELPER_SECRET", "job-spoof")
	empty := t.TempDir()
	client := CLI{Path: path, ConfigDir: empty, AuthConfigDir: dir, HelperPath: "/usr/bin:/bin", HelperEnvironment: auth.Environment}
	for _, action := range []string{"pull", "create", "info"} {
		var out bytes.Buffer
		env := map[string]string(nil)
		if action == "create" {
			env = map[string]string{"JOB_SECRET": "job-value"}
		}
		if _, err := client.Run(t.Context(), []string{action, "image"}, env, &out, &out); err != nil {
			t.Fatal(err)
		}
		result := out.String()
		if action == "pull" {
			if !strings.Contains(result, "\n"+dir+"\n") || !strings.Contains(result, "HELPER:synthetic-helper-secret") || !strings.Contains(result, "JOB:unset") {
				t.Fatalf("pull environment: %s", result)
			}
		} else if !strings.Contains(result, "\n"+empty+"\n") || !strings.Contains(result, "HELPER:unset") {
			t.Fatalf("runtime environment: %s", result)
		}
	}
}

func TestAuthenticationValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	for _, content := range []string{"invalid", `{"auths":42}`} {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadAuthentication(dir, "", ""); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	if err := os.Chmod(filepath.Join(dir, "config.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAuthentication(dir, "", ""); err == nil {
		t.Fatal("accepted public credentials")
	}
	if _, err := LoadAuthentication("relative", "", ""); err == nil {
		t.Fatal("accepted relative config")
	}
	if _, err := LoadAuthentication("", "/helpers.json", ""); err == nil {
		t.Fatal("accepted helpers without config")
	}
}

func TestCredentialsCannotBeMounted(t *testing.T) {
	cfg := testConfig(t)
	dir := t.TempDir()
	cfg.DockerConfig = dir
	for _, source := range []string{dir, filepath.Dir(dir)} {
		if err := rejectCredentialMount(source, cfg); err == nil {
			t.Fatal("accepted credential mount")
		}
	}
	link := filepath.Join(t.TempDir(), "credentials")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := rejectCredentialMount(link, cfg); err == nil {
		t.Fatal("accepted symlink credential mount")
	}
	if err := rejectCredentialMount(t.TempDir(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryDiagnosticRedaction(t *testing.T) {
	secret := "synthetic-registry-secret"
	f := &fakeClient{run: func(_ context.Context, _ []string, _ map[string]string, _, stderr io.Writer) (int, error) {
		_, _ = fmt.Fprintln(stderr, "credential failure:", secret)
		return 1, fmt.Errorf("helper failed: %s", secret)
	}}
	client := withDiagnostics(f, nil, secret)
	_, err := client.Run(t.Context(), []string{"pull", "registry.test/image"}, nil, io.Discard, io.Discard)
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
}
