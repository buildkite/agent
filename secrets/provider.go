package secrets

import (
	"encoding/json"
	"fmt"
)

type providerCandidate struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Config json.RawMessage `json:"config"`
}

func (r providerCandidate) Initialize() (Provider, error) {
	switch r.Type {
	case "aws-ssm":
		var conf AWSSSMProviderConfig
		err := json.Unmarshal(r.Config, &conf)
		if err != nil {
			return nil, fmt.Errorf("unmarshalling config for aws-ssm provider %s: %v", r.ID, err)
		}

		ssm, err := NewAWSSSMProvider(r.ID, conf)
		if err != nil {
			return nil, fmt.Errorf("creating aws-ssm provider %s: %w", r.ID, err)
		}

		return ssm, nil
	default:
		return nil, fmt.Errorf("invalid provider type %s for provider %s", r.Type, r.ID)
	}
}

// A Provider is a source of secrets, and must provide a way to fetch a secret given some key. Providers must be goroutine-safe.
type Provider interface {
	Fetch(key string) (string, error)
}
