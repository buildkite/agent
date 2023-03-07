package secrets

import "fmt"

type SecretConfig struct {
	Key        string `json:"key"`
	ProviderID string `json:"provider_id"`
	EnvVar     string `json:"env_var"`
	File       string `json:"file"`
}

func (s SecretConfig) Validate() error {
	if s.Key == "" {
		return fmt.Errorf("secret: %s (provider: %s): key is required", s.Key, s.ProviderID)
	}

	if s.ProviderID == "" {
		return fmt.Errorf("secret: %s (provider: %s): provider_id is required", s.Key, s.ProviderID)
	}

	if s.EnvVar == "" && s.File == "" {
		return fmt.Errorf("secret: %s (provider: %s): must have either env_var or file set", s.Key, s.ProviderID)
	}

	if s.EnvVar != "" && s.File != "" {
		return fmt.Errorf("secret: %s (provider: %s): must only have one of env_var or file set", s.Key, s.ProviderID)
	}

	return nil
}

func (s SecretConfig) Type() string {
	if s.EnvVar != "" {
		return "env"
	}

	return "file"
}
