package dockerexec

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/moby/moby/api/types/registry"
)

func TestRegistryAuth(t *testing.T) {
	t.Parallel()

	userPass := base64.StdEncoding.EncodeToString([]byte("alice:s3cr:et"))
	config := `{
		"auths": {
			"https://index.docker.io/v1/": {"auth": "` + userPass + `"},
			"registry.example.com": {"username": "bob", "password": "hunter2"},
			"https://ghcr.io": {"identitytoken": "tok"},
			"localhost:5000": {"username": "local", "password": "pw"},
			"http://Mirror.Example.com:8080/v2/": {"username": "mirror", "password": "pw"},
			"empty.example.com": {},
			"quay.io": {}
		},
		"credsStore": "desktop"
	}`

	tests := []struct {
		image string
		want  *registry.AuthConfig
	}{
		{image: "debian:stable-slim", want: &registry.AuthConfig{Username: "alice", Password: "s3cr:et", ServerAddress: "https://index.docker.io/v1/"}},
		{image: "docker.io/library/debian", want: &registry.AuthConfig{Username: "alice", Password: "s3cr:et", ServerAddress: "https://index.docker.io/v1/"}},
		{image: "registry.example.com/team/app:1.2", want: &registry.AuthConfig{Username: "bob", Password: "hunter2", ServerAddress: "registry.example.com"}},
		{image: "ghcr.io/org/app@sha256:" + strings.Repeat("a", 64), want: &registry.AuthConfig{IdentityToken: "tok", ServerAddress: "https://ghcr.io"}},
		{image: "localhost:5000/app", want: &registry.AuthConfig{Username: "local", Password: "pw", ServerAddress: "localhost:5000"}},
		{image: "mirror.example.com:8080/app", want: &registry.AuthConfig{Username: "mirror", Password: "pw", ServerAddress: "http://Mirror.Example.com:8080/v2/"}},
		{image: "quay.io/org/app", want: nil},
		{image: "empty.example.com/app", want: nil},
	}
	dir := t.TempDir()
	writeDockerConfig(t, dir, config)
	for _, tc := range tests {
		got, err := registryAuth(tc.image, dir)
		if err != nil {
			t.Errorf("registryAuth(%q) error = %v", tc.image, err)
			continue
		}
		if diff := cmp.Diff(tc.want, decodeAuth(t, got)); diff != "" {
			t.Errorf("registryAuth(%q) diff (-want +got):\n%s", tc.image, diff)
		}
	}
}

func TestRegistryAuth_NoConfig(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"", t.TempDir()} {
		got, err := registryAuth("debian", dir)
		if err != nil || got != "" {
			t.Errorf("registryAuth(debian, %q) = (%q, %v), want anonymous", dir, got, err)
		}
	}
}

func TestRegistryAuth_EmptyEntryDoesNotHideAUsableOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeDockerConfig(t, dir, `{"auths": {"docker.io": {}, "https://index.docker.io/v1/": {"username": "alice", "password": "pw"}}}`)
	got, err := registryAuth("debian", dir)
	if err != nil {
		t.Fatalf("registryAuth() error = %v", err)
	}
	want := &registry.AuthConfig{Username: "alice", Password: "pw", ServerAddress: "https://index.docker.io/v1/"}
	if diff := cmp.Diff(want, decodeAuth(t, got)); diff != "" {
		t.Errorf("registryAuth() diff (-want +got):\n%s", diff)
	}
}

func TestRegistryAuth_MalformedAuth(t *testing.T) {
	t.Parallel()

	for _, auth := range []string{"!!!not-base64", base64.StdEncoding.EncodeToString([]byte("no-colon-secret"))} {
		dir := t.TempDir()
		writeDockerConfig(t, dir, `{"auths": {"docker.io": {"auth": "`+auth+`"}}}`)
		_, err := registryAuth("debian", dir)
		if err == nil {
			t.Errorf("registryAuth() with auth %q error = nil, want an error", auth)
			continue
		}
		if strings.Contains(err.Error(), "no-colon-secret") {
			t.Errorf("registryAuth() error %q leaks the credential", err)
		}
	}
}

func TestRegistryAuth_InvalidImage(t *testing.T) {
	t.Parallel()

	if _, err := registryAuth("Not A Valid:Ref", t.TempDir()); err == nil {
		t.Error("registryAuth(invalid) error = nil, want an error")
	}
}

// decodeAuth decodes an X-Registry-Auth value, or returns nil for "".
func decodeAuth(t *testing.T, encoded string) *registry.AuthConfig {
	t.Helper()
	if encoded == "" {
		return nil
	}
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode error = %v", err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return &auth
}
