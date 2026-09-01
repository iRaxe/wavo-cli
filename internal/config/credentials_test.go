package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/zalando/go-keyring"
)

func TestCheckKeyringAvailable(t *testing.T) {
	keyring.MockInit()
	if err := CheckKeyringAvailable(); err != nil {
		t.Fatalf("CheckKeyringAvailable() error = %v", err)
	}
}

func TestCheckKeyringAvailableReturnsBackendFailure(t *testing.T) {
	backendErr := fmt.Errorf("secret service is unavailable")
	keyring.MockInitWithError(backendErr)

	err := CheckKeyringAvailable()
	if !errors.Is(err, backendErr) {
		t.Fatalf("CheckKeyringAvailable() error = %v, want backend failure", err)
	}
}

func initMockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	if err := keyring.DeleteAll(keyringService); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	t.Setenv(APIKeyEnv, "")
}

func TestResolveAPIKeyPrefersEnvironment(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 123}

	if err := SaveAPIKey(cfg, "keyring-token"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}
	t.Setenv(APIKeyEnv, "env-token")

	apiKey, source, err := ResolveAPIKey(cfg)
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if apiKey != "env-token" {
		t.Fatalf("apiKey = %q, want env-token", apiKey)
	}
	if source != CredentialSourceEnvironment {
		t.Fatalf("source = %q, want %q", source, CredentialSourceEnvironment)
	}
}

func TestAPIKeyEnvironmentUsesWavoBrand(t *testing.T) {
	if APIKeyEnv != brand.APIKeyEnv {
		t.Fatalf("APIKeyEnv = %q, want %q", APIKeyEnv, brand.APIKeyEnv)
	}
}

func TestResolveAPIKeyFromKeyring(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example/", AccountID: 124}

	if err := SaveAPIKey(cfg, "keyring-token"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	apiKey, source, err := ResolveAPIKey(&Config{BaseURL: "https://wavo.example", AccountID: 124})
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if apiKey != "keyring-token" {
		t.Fatalf("apiKey = %q, want keyring-token", apiKey)
	}
	if source != CredentialSourceKeyring {
		t.Fatalf("source = %q, want %q", source, CredentialSourceKeyring)
	}

	stored, err := keyring.Get(keyringService, apiKeyKeyringEntry)
	if err != nil {
		t.Fatalf("keyring.Get() error = %v", err)
	}
	var credential savedCredential
	if err := json.Unmarshal([]byte(stored), &credential); err != nil {
		t.Fatalf("stored credential is not JSON: %v", err)
	}
	if credential.BaseURL != "https://wavo.example" || credential.AccountID != 124 || credential.APIKey != "keyring-token" {
		t.Fatalf("stored credential = %#v, want scoped api key", credential)
	}
}

func TestSaveAPIKeyWithRollbackRestoresPreviousCredential(t *testing.T) {
	initMockKeyring(t)
	oldConfig := &Config{BaseURL: brand.DefaultBaseURL, AccountID: 7}
	newConfig := &Config{BaseURL: "https://other.wavo.example", AccountID: 9}
	if err := SaveAPIKey(oldConfig, "old-token"); err != nil {
		t.Fatalf("SaveAPIKey(old) error = %v", err)
	}
	previous, err := keyring.Get(keyringService, apiKeyKeyringEntry)
	if err != nil {
		t.Fatalf("keyring.Get(previous) error = %v", err)
	}
	if err := keyring.Set(keyringService, "unrelated-entry", "keep-me"); err != nil {
		t.Fatalf("keyring.Set(unrelated) error = %v", err)
	}

	rollback, err := SaveAPIKeyWithRollback(newConfig, "new-token")
	if err != nil {
		t.Fatalf("SaveAPIKeyWithRollback() error = %v", err)
	}
	if got, _, err := ResolveAPIKey(newConfig); err != nil || got != "new-token" {
		t.Fatalf("new credential not saved: got=%q err=%v", got, err)
	}
	if err := rollback(); err != nil {
		t.Fatalf("rollback() error = %v", err)
	}

	restored, err := keyring.Get(keyringService, apiKeyKeyringEntry)
	if err != nil || restored != previous {
		t.Fatalf("rollback restored %q, %v; want exact previous entry %q", restored, err, previous)
	}
	if got, _, err := ResolveAPIKey(oldConfig); err != nil || got != "old-token" {
		t.Fatalf("old credential not usable after rollback: got=%q err=%v", got, err)
	}
	if got, err := keyring.Get(keyringService, "unrelated-entry"); err != nil || got != "keep-me" {
		t.Fatalf("rollback changed unrelated entry: got=%q err=%v", got, err)
	}
}

func TestSaveAPIKeyWithRollbackDeletesOnlyNewCredential(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: brand.DefaultBaseURL, AccountID: 7}
	if err := keyring.Set(keyringService, "unrelated-entry", "keep-me"); err != nil {
		t.Fatalf("keyring.Set(unrelated) error = %v", err)
	}

	rollback, err := SaveAPIKeyWithRollback(cfg, "new-token")
	if err != nil {
		t.Fatalf("SaveAPIKeyWithRollback() error = %v", err)
	}
	if err := rollback(); err != nil {
		t.Fatalf("rollback() error = %v", err)
	}

	if _, err := keyring.Get(keyringService, apiKeyKeyringEntry); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("new credential remains after rollback: %v", err)
	}
	if got, err := keyring.Get(keyringService, "unrelated-entry"); err != nil || got != "keep-me" {
		t.Fatalf("rollback changed unrelated entry: got=%q err=%v", got, err)
	}
}

func TestResolveAPIKeyRejectsMismatchedStableCredential(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 124}

	if err := SaveAPIKey(cfg, "keyring-token"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	_, source, err := ResolveAPIKey(&Config{BaseURL: "https://evil.example", AccountID: 124})
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("ResolveAPIKey() error = %v, want ErrAPIKeyNotFound", err)
	}
	if source != CredentialSourceMissing {
		t.Fatalf("source = %q, want %q", source, CredentialSourceMissing)
	}
}

func TestResolveAPIKeyRejectsRawStableCredential(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 124}

	if err := keyring.Set(keyringService, apiKeyKeyringEntry, "raw-token"); err != nil {
		t.Fatalf("keyring.Set() error = %v", err)
	}

	_, source, err := ResolveAPIKey(cfg)
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("ResolveAPIKey() error = %v, want ErrAPIKeyNotFound", err)
	}
	if source != CredentialSourceMissing {
		t.Fatalf("source = %q, want %q", source, CredentialSourceMissing)
	}
}

func TestResolveAPIKeyMissing(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 125}

	_, source, err := ResolveAPIKey(cfg)
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("ResolveAPIKey() error = %v, want ErrAPIKeyNotFound", err)
	}
	if source != CredentialSourceMissing {
		t.Fatalf("source = %q, want %q", source, CredentialSourceMissing)
	}
}

func TestMissingAPIKeyErrorUsesWavoLogin(t *testing.T) {
	initMockKeyring(t)

	_, _, err := ResolveAPIKey(&Config{BaseURL: brand.DefaultBaseURL, AccountID: 7})
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("ResolveAPIKey() error = %v, want ErrAPIKeyNotFound", err)
	}
	for _, want := range []string{"wavo auth login", brand.APIKeyEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ResolveAPIKey() error = %q, want substring %q", err, want)
		}
	}
}

func TestDeleteAPIKeyRemovesKeyringToken(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 126}

	if err := SaveAPIKey(cfg, "keyring-token"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}
	if err := DeleteAPIKey(cfg); err != nil {
		t.Fatalf("DeleteAPIKey() error = %v", err)
	}

	_, _, err := ResolveAPIKey(cfg)
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("ResolveAPIKey() error = %v, want ErrAPIKeyNotFound", err)
	}
}

func TestDeleteAPIKeyRemovesAllServiceEntries(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 128}

	if err := keyring.Set(keyringService, apiKeyKeyringEntry, "stable-token"); err != nil {
		t.Fatalf("keyring.Set(stable) error = %v", err)
	}
	if err := keyring.Set(keyringService, "stale-entry", "stale-token"); err != nil {
		t.Fatalf("keyring.Set(stale) error = %v", err)
	}

	if err := DeleteAPIKey(cfg); err != nil {
		t.Fatalf("DeleteAPIKey() error = %v", err)
	}

	for name, key := range map[string]string{
		"stable": apiKeyKeyringEntry,
		"stale":  "stale-entry",
	} {
		if _, err := keyring.Get(keyringService, key); !errors.Is(err, keyring.ErrNotFound) {
			t.Fatalf("%s token still present after DeleteAPIKey, err = %v", name, err)
		}
	}
}

func TestDeleteAPIKeyDoesNotDeleteOtherServiceCredentials(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(func() { _ = keyring.DeleteAll("other-cli") })
	if err := keyring.Set("other-cli", "api-key", "other-secret"); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BaseURL: brand.DefaultBaseURL, AccountID: 7}
	if err := SaveAPIKey(cfg, "wavo-secret"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAPIKey(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := keyring.Get("other-cli", "api-key")
	if err != nil || got != "other-secret" {
		t.Fatalf("other credential changed: got=%q err=%v", got, err)
	}
}

// TestDeleteAPIKeyLeavesOtherProfileServiceIntact guards the dev/prod isolation
// guarantee: keyringService is namespaced per build profile, so logging out of
// one build must not delete the other build's saved token. DeleteAPIKey wipes
// only keyringService, never another service's entries.
func TestDeleteAPIKeyLeavesOtherProfileServiceIntact(t *testing.T) {
	initMockKeyring(t)
	cfg := &Config{BaseURL: "https://wavo.example", AccountID: 130}

	otherProfileService := brand.KeyringService
	if !IsDev {
		otherProfileService = brand.DevKeyringService
	}
	if err := keyring.Set(otherProfileService, apiKeyKeyringEntry, "other-profile-token"); err != nil {
		t.Fatalf("seed other-profile service: %v", err)
	}
	if err := SaveAPIKey(cfg, "this-profile-token"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	if err := DeleteAPIKey(cfg); err != nil {
		t.Fatalf("DeleteAPIKey() error = %v", err)
	}

	// Active profile's token is gone...
	if _, _, err := ResolveAPIKey(cfg); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("active profile token survived logout, err = %v", err)
	}
	// ...but the other profile's token must be untouched.
	got, err := keyring.Get(otherProfileService, apiKeyKeyringEntry)
	if err != nil {
		t.Fatalf("other profile token erased by logout: %v", err)
	}
	if got != "other-profile-token" {
		t.Fatalf("other profile token = %q, want other-profile-token", got)
	}
}
