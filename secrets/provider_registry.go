package secrets

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/puzpuzpuz/xsync"
)

type ProviderRegistry struct {
	config ProviderRegistryConfig
	// We have the two maps here because we only want to initialise a provider if a secret using that provider is used
	// We shouldn't boot up any secrets providers that won't get used
	candidates *xsync.MapOf[string, providerCandidate] // Candidates technically doesn't need to be a sync map as it's never altered after initialization, but i've made it one just for symmetry
	providers  *xsync.MapOf[string, Provider]
}

type ProviderRegistryConfig struct {
	Shell *shell.Shell
}

// NewProviderRegistryFromJSON takes a JSON string representing a slice of secrets.RawProvider, and returns a ProviderRegistry,
// ready to be used to fetch secrets.
func NewProviderRegistryFromJSON(config ProviderRegistryConfig, jsonIn string) (*ProviderRegistry, error) {
	var rawProviders []providerCandidate
	err := json.Unmarshal([]byte(jsonIn), &rawProviders)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling secret providers: %w", err)
	}

	candidates := xsync.NewMapOf[providerCandidate]()
	for _, provider := range rawProviders {
		if _, ok := candidates.Load(provider.ID); ok {
			return nil, fmt.Errorf("duplicate provider ID: %s. Provider IDs must be unique", provider.ID)
		}

		candidates.Store(provider.ID, provider)
	}

	return &ProviderRegistry{
		config:     config,
		candidates: candidates,
		providers:  xsync.NewMapOf[Provider](),
	}, nil
}

func (pr *ProviderRegistry) FetchAll(configs []SecretConfig) ([]Secret, []error) {
	secrets := make([]Secret, 0, len(configs))
	errors := make([]error, 0, len(configs))

	var (
		wg  sync.WaitGroup
		mtx sync.Mutex
	)

	wg.Add(len(configs))
	for _, c := range configs {
		go func(config SecretConfig) {
			defer wg.Done()

			secret, err := pr.Fetch(config)
			mtx.Lock()
			defer mtx.Unlock()

			if err != nil {
				errors = append(errors, err)
				return
			}

			secrets = append(secrets, secret)
		}(c)
	}

	wg.Wait()

	return secrets, errors
}

// Fetch takes a SecretConfig, and attempts to fetch it from the provider specified in the config.
// This method is goroutine-safe.
func (pr *ProviderRegistry) Fetch(config SecretConfig) (Secret, error) {
	if provider, ok := pr.providers.Load(config.ProviderID); ok { // We've used this provider before, it's already been initialized
		value, err := provider.Fetch(config.Key)
		if err != nil {
			return nil, fmt.Errorf("fetching secret %s from provider %s: %w", config.Key, config.ProviderID, err)
		}

		pr.config.Shell.Commentf("Secret %s fetched from provider %s", config.Key, config.ProviderID)
		secret, err := newSecret(config, pr.config.Shell.Env, value)
		if err != nil {
			return nil, fmt.Errorf("creating secret %s from provider %s: %w", config.Key, config.ProviderID, err)
		}
		return secret, nil
	}

	if candidate, ok := pr.candidates.Load(config.ProviderID); ok { // We haven't used this provider yet, so we need to initialize it
		provider, err := candidate.Initialize()
		if err != nil {
			return nil, fmt.Errorf("initializing provider %s (type: %s) to fetch secret %s: %w", config.ProviderID, candidate.Type, config.Key, err)
		}

		pr.providers.Store(config.ProviderID, provider) // Store the initialised provider

		value, err := provider.Fetch(config.Key) // Now fetch the actual secret.
		if err != nil {
			return nil, fmt.Errorf("fetching secret %s from provider %s: %w", config.Key, config.ProviderID, err)
		}

		pr.config.Shell.Commentf("Secret %s fetched from provider %s", config.Key, config.ProviderID)
		secret, err := newSecret(config, pr.config.Shell.Env, value)
		if err != nil {
			return nil, fmt.Errorf("creating secret %s from provider %s: %w", config.Key, config.ProviderID, err)
		}
		return secret, nil
	}

	// If we've got to this point, the user has tried to use a provider ID that's not in the registry, so we can't give them their secret
	return nil, fmt.Errorf("no secret provider with ID: %s", config.ProviderID)
}
