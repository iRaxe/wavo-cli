package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/zalando/go-keyring"
)

const (
	APIKeyEnv          = brand.APIKeyEnv
	apiKeyKeyringEntry = "api-key"
)

// keyringService is build-profile specific ("wavo-cli" for prod,
// "wavo-cli-dev" for dev). Namespacing the whole service per profile keeps
// logout's DeleteAll(keyringService) scoped to the active build, so a dev logout
// can't erase a prod login's token and vice versa. See profile_prod.go /
// profile_dev.go.

type CredentialSource string

const (
	CredentialSourceEnvironment CredentialSource = "environment"
	CredentialSourceKeyring     CredentialSource = "keyring"
	CredentialSourceMissing     CredentialSource = "missing"
)

var ErrAPIKeyNotFound = errors.New("api key not found")

type savedCredential struct {
	BaseURL   string `json:"base_url"`
	AccountID int    `json:"account_id"`
	APIKey    string `json:"api_key"`
}

// ResolveAPIKey implements the auth flow for the CLI. YAML config intentionally
// stores only non-secrets, and plaintext api_key values from older configs are
// ignored. WAVO_API_KEY wins for CI, coding agents, and temporary overrides;
// otherwise saved interactive logins read the token from the OS keyring.
func ResolveAPIKey(cfg *Config) (string, CredentialSource, error) {
	if apiKey := strings.TrimSpace(os.Getenv(APIKeyEnv)); apiKey != "" {
		return apiKey, CredentialSourceEnvironment, nil
	}

	if cfg == nil || !cfg.IsValid() {
		return "", CredentialSourceMissing, missingAPIKeyError()
	}

	stored, err := keyring.Get(keyringService, apiKeyKeyringEntry)
	if err == nil {
		apiKey, err := parseSavedCredential(stored, cfg)
		if err != nil {
			return "", CredentialSourceMissing, err
		}
		return apiKey, CredentialSourceKeyring, nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return "", CredentialSourceMissing, fmt.Errorf("failed to read API key from keyring: %w", err)
	}

	return "", CredentialSourceMissing, missingAPIKeyError()
}

func SaveAPIKey(cfg *Config, apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api key is required")
	}
	if cfg == nil || !cfg.IsValid() {
		return fmt.Errorf("valid config is required to save API key")
	}
	if err := saveAPIKeyToKeyring(cfg, apiKey); err != nil {
		return fmt.Errorf("failed to save API key to keyring: %w", err)
	}
	return nil
}

// SaveAPIKeyWithRollback replaces the active profile's credential and returns
// a rollback that restores the exact previous entry, or removes only the entry
// created by this save. Other entries in the Wavo keyring service are untouched.
func SaveAPIKeyWithRollback(cfg *Config, apiKey string) (func() error, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	if cfg == nil || !cfg.IsValid() {
		return nil, fmt.Errorf("valid config is required to save API key")
	}

	previous, err := keyring.Get(keyringService, apiKeyKeyringEntry)
	hadPrevious := err == nil
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("failed to read previous API key from keyring: %w", err)
	}
	if err := saveAPIKeyToKeyring(cfg, apiKey); err != nil {
		return nil, fmt.Errorf("failed to save API key to keyring: %w", err)
	}

	rollback := func() error {
		if hadPrevious {
			if err := keyring.Set(keyringService, apiKeyKeyringEntry, previous); err != nil {
				return fmt.Errorf("failed to restore previous API key in keyring: %w", err)
			}
			return nil
		}
		if err := keyring.Delete(keyringService, apiKeyKeyringEntry); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("failed to remove new API key from keyring: %w", err)
		}
		return nil
	}
	return rollback, nil
}

func saveAPIKeyToKeyring(cfg *Config, apiKey string) error {
	credential := savedCredential{
		BaseURL:   normalizeBaseURL(cfg.BaseURL),
		AccountID: cfg.AccountID,
		APIKey:    apiKey,
	}

	data, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, apiKeyKeyringEntry, string(data))
}

func parseSavedCredential(stored string, cfg *Config) (string, error) {
	var credential savedCredential
	if err := json.Unmarshal([]byte(stored), &credential); err != nil {
		return "", credentialScopeMismatchError()
	}
	if credential.APIKey == "" ||
		credential.BaseURL != normalizeBaseURL(cfg.BaseURL) ||
		credential.AccountID != cfg.AccountID {
		return "", credentialScopeMismatchError()
	}
	return credential.APIKey, nil
}

// DeleteAPIKey removes every credential saved by this CLI service. This avoids
// leaving stale keyring entries behind when config.yaml was edited or removed.
func DeleteAPIKey(_ *Config) error {
	err := keyring.DeleteAll(keyringService)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("failed to delete API keys from keyring: %w", err)
}

func missingAPIKeyError() error {
	return fmt.Errorf("%w; run '%s auth login' or set %s", ErrAPIKeyNotFound, brand.Command, APIKeyEnv)
}

func credentialScopeMismatchError() error {
	return fmt.Errorf("%w; saved keyring credential does not match configured %s instance; run '%s auth login' for this base URL and account", ErrAPIKeyNotFound, brand.Product, brand.Command)
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
