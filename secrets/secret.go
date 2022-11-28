package secrets

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/env"
)

type Secret interface {
	Store() error
	Cleanup() error
}

func newSecret(config SecretConfig, environment env.Environment, value string) (Secret, error) {
	switch config.Type() {
	case "env":
		return newEnvSecret(config, environment, value), nil
	case "file":
		return newFileSecret(config, value), nil
	default:
		return nil, fmt.Errorf("invalid secret type %s", config.Type())
	}
}

type EnvSecret struct {
	EnvVar string
	Value  string
	Env    env.Environment
}

func newEnvSecret(config SecretConfig, environment env.Environment, value string) Secret {
	return EnvSecret{
		EnvVar: config.EnvVar,
		Value:  value,
		Env:    environment,
	}
}

func (s EnvSecret) Store() error {
	s.Env.Set(s.EnvVar, s.Value)
	return nil
}

func (s EnvSecret) Cleanup() error {
	return nil
}

type FileSecret struct {
	FilePath string
	Value    string
}

func newFileSecret(config SecretConfig, value string) Secret {
	return FileSecret{
		FilePath: config.File,
		Value:    value,
	}
}

func (s FileSecret) Store() error {
	return os.WriteFile(s.FilePath, []byte(s.Value), 0777)
}

func (s FileSecret) Cleanup() error {
	return os.Remove(s.FilePath)
}
