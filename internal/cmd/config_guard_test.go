package cmd

import (
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/config"
)

func TestUpdateConfigForAuthNormalizesBaseURLAndPreservesCurrentFields(t *testing.T) {
	setupTestEnv(t)
	snapshot := &config.Config{BaseURL: "HTTPS://WAVO.EXAMPLE/", AccountID: 7, UserID: 77}
	current := &config.Config{
		BaseURL:   "https://wavo.example",
		AccountID: 7,
		UserID:    77,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "existing",
			DefaultLocale:     "it",
		},
	}
	if err := config.Save(current); err != nil {
		t.Fatalf("Save(current): %v", err)
	}

	updated, err := updateConfigForAuth(snapshot, func(fresh *config.Config) error {
		fresh.HelpCenter.DefaultPortalSlug = "updated"
		return nil
	})
	if err != nil {
		t.Fatalf("updateConfigForAuth: %v", err)
	}
	if updated.UserID != 77 || updated.HelpCenter.DefaultLocale != "it" || updated.HelpCenter.DefaultPortalSlug != "updated" {
		t.Fatalf("updated config did not preserve fresh fields: %#v", updated)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *updated {
		t.Fatalf("Load() = %#v, %v; want %#v", loaded, err, updated)
	}
}

func TestUpdateConfigForAuthRejectsChangedUserID(t *testing.T) {
	setupTestEnv(t)
	snapshot := &config.Config{BaseURL: "https://wavo.example/", AccountID: 7}
	current := &config.Config{
		BaseURL:   "https://wavo.example",
		AccountID: 7,
		UserID:    77,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "new-login-portal",
			DefaultLocale:     "it",
		},
	}
	if err := config.Save(current); err != nil {
		t.Fatalf("Save(current): %v", err)
	}

	updated, err := updateConfigForAuth(snapshot, func(fresh *config.Config) error {
		fresh.HelpCenter = config.HelpCenterConfig{}
		return nil
	})
	if updated != nil || err == nil || !strings.Contains(err.Error(), "Wavo") || !strings.Contains(strings.ToLower(err.Error()), "retry") {
		t.Fatalf("updateConfigForAuth = (%#v, %v), want actionable identity-change error", updated, err)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *current {
		t.Fatalf("stale mutation changed new identity config: cfg=%#v err=%v", loaded, err)
	}
}
