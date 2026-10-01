package dockerbootstrap

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Authentication is loaded from operator files, never the inherited job environment.
type Authentication struct {
	Config      []byte
	Environment map[string]string
	Secrets     []string
}

func LoadAuthentication(directory, envFile, helperPath string) (Authentication, error) {
	var result Authentication
	if directory == "" {
		if envFile != "" || helperPath != "" {
			return result, fmt.Errorf("credential helper options require --docker-config")
		}
		return result, nil
	}
	if !filepath.IsAbs(directory) {
		return result, fmt.Errorf("docker-config must be an absolute directory")
	}
	if helperPath != "" {
		for _, path := range filepath.SplitList(helperPath) {
			if !filepath.IsAbs(path) {
				return result, fmt.Errorf("docker-helper-path requires absolute directories")
			}
		}
	}
	data, err := readCredentialFile(filepath.Join(directory, "config.json"))
	if err != nil {
		return result, err
	}
	var config struct {
		Auths map[string]struct {
			Auth          string `json:"auth,omitempty"`
			Username      string `json:"username,omitempty"`
			Password      string `json:"password,omitempty"`
			IdentityToken string `json:"identitytoken,omitempty"`
			RegistryToken string `json:"registrytoken,omitempty"`
		} `json:"auths,omitempty"`
		CredsStore  string            `json:"credsStore,omitempty"`
		CredHelpers map[string]string `json:"credHelpers,omitempty"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return result, fmt.Errorf("docker config must contain valid JSON")
	}
	for _, auth := range config.Auths {
		for _, value := range []string{auth.Auth, auth.Password, auth.IdentityToken, auth.RegistryToken} {
			if value != "" {
				result.Secrets = append(result.Secrets, value)
			}
		}
		if decoded, err := base64.StdEncoding.DecodeString(auth.Auth); err == nil && len(decoded) > 0 {
			result.Secrets = append(result.Secrets, string(decoded))
			if _, password, ok := strings.Cut(string(decoded), ":"); ok && password != "" {
				result.Secrets = append(result.Secrets, password)
			}
		}
	}
	// Proxy injection, CLI plugins, and custom headers are not registry credentials.
	result.Config, err = json.Marshal(config)
	if err != nil {
		return result, err
	}
	if envFile != "" {
		data, err := readCredentialFile(envFile)
		if err != nil {
			return result, err
		}
		if err := json.Unmarshal(data, &result.Environment); err != nil {
			return result, fmt.Errorf("helper environment must be a JSON object of string values")
		}
		for name, value := range result.Environment {
			if !envName.MatchString(name) || strings.HasPrefix(name, "DOCKER_") || name == "PATH" || strings.ContainsRune(value, 0) {
				return result, fmt.Errorf("invalid helper environment; Docker settings and PATH cannot be overridden")
			}
			if value != "" {
				result.Secrets = append(result.Secrets, value)
			}
		}
	}
	return result, nil
}

func readCredentialFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("credential file paths must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("credential files must be regular files accessible only to their owner")
	}
	return os.ReadFile(path)
}

func rejectCredentialMount(source string, cfg Config) error {
	if cfg.DockerConfig == "" && cfg.HelperEnvFile == "" {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	for _, secret := range []string{cfg.DockerConfig, cfg.HelperEnvFile} {
		if secret == "" {
			continue
		}
		target, err := filepath.EvalSymlinks(secret)
		if err != nil {
			return fmt.Errorf("resolve credential location: %w", err)
		}
		if within(target, resolved) || within(resolved, target) {
			return fmt.Errorf("docker mount overlaps host registry credentials")
		}
	}
	return nil
}
