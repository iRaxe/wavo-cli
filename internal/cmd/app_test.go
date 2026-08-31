package cmd

import (
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/config"
)

func TestNewAppRecordsCredentialSource(t *testing.T) {
	for _, test := range []struct {
		name    string
		envKey  string
		keyring string
		want    config.CredentialSource
	}{
		{name: "environment", envKey: "environment-secret", want: config.CredentialSourceEnvironment},
		{name: "keyring", keyring: "keyring-secret", want: config.CredentialSourceKeyring},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTestEnv(t)
			t.Setenv(config.APIKeyEnv, test.envKey)
			cfg := &config.Config{BaseURL: "https://wavo.example", AccountID: 7, UserID: 11}
			if err := config.Save(cfg); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if test.keyring != "" {
				if err := config.SaveAPIKey(cfg, test.keyring); err != nil {
					t.Fatalf("SaveAPIKey: %v", err)
				}
			}

			app, err := NewApp(&CLI{Output: "text"}, false, "test")
			if err != nil {
				t.Fatalf("NewApp: %v", err)
			}
			if app.CredentialSource != test.want {
				t.Fatalf("CredentialSource = %q, want %q", app.CredentialSource, test.want)
			}
			for _, secret := range []string{test.envKey, test.keyring} {
				if secret != "" && strings.Contains(string(app.CredentialSource), secret) {
					t.Fatalf("credential source exposed token %q", secret)
				}
			}
		})
	}
}

func TestNewAppSkipAuthUsesMissingCredentialSource(t *testing.T) {
	app, err := NewApp(&CLI{Output: "text"}, true, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if app.CredentialSource != config.CredentialSourceMissing {
		t.Fatalf("CredentialSource = %q, want missing", app.CredentialSource)
	}
}
