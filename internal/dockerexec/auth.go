package dockerexec

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"
)

// registryAuth returns the encoded registry credentials for pulling image,
// taken from the static "auths" entries in the Docker config file in
// configDir. It returns "" to pull anonymously when there is no config file
// or no matching entry. Credential helpers are not supported.
func registryAuth(image, configDir string) (string, error) {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", fmt.Errorf("executor-docker-image %q: %w", image, err)
	}

	if configDir == "" {
		return "", nil
	}
	path := filepath.Join(configDir, "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading Docker config: %w", err)
	}
	var config struct {
		Auths map[string]struct {
			Auth          string `json:"auth"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			IdentityToken string `json:"identitytoken"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parsing Docker config %s: %w", path, err)
	}

	host := strings.ToLower(reference.Domain(named))
	keys := make([]string, 0, len(config.Auths))
	for key := range config.Auths {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		entry := config.Auths[key]
		// docker login with a credential store writes empty entries, which
		// would otherwise hide a usable one for the same registry. An entry
		// with only a password is unusable too.
		if registryHost(key) != host || (entry.Auth == "" && entry.Username == "" && entry.IdentityToken == "") {
			continue
		}
		auth := registry.AuthConfig{
			Username:      entry.Username,
			Password:      entry.Password,
			IdentityToken: entry.IdentityToken,
			ServerAddress: key,
		}
		if entry.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				return "", fmt.Errorf("parsing Docker config %s: auth for %s is not valid base64", path, key)
			}
			var ok bool
			auth.Username, auth.Password, ok = strings.Cut(string(decoded), ":")
			if !ok {
				return "", fmt.Errorf("parsing Docker config %s: auth for %s is not username:password", path, key)
			}
		}
		return authconfig.Encode(auth)
	}
	return "", nil
}

// registryHost turns a Docker config "auths" key, which may be a bare host
// or a URL such as https://index.docker.io/v1/, into the registry host that
// reference.Domain returns.
func registryHost(key string) string {
	key = strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
	host, _, _ := strings.Cut(key, "/")
	host = strings.ToLower(host)
	switch host {
	case "index.docker.io", "registry-1.docker.io":
		return "docker.io"
	}
	return host
}

// dockerConfigDir returns the directory the Docker CLI reads its config
// from: $DOCKER_CONFIG, or ~/.docker.
func dockerConfigDir() string {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".docker")
}
