package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/iRaxe/wavo-cli/internal/config"
	"github.com/iRaxe/wavo-cli/internal/lock"
	"github.com/iRaxe/wavo-cli/internal/output"
)

func TestHCDefaultSavesPortalAndLocale(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/portals" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"payload": [{
				"id": 1,
				"name": "Wavo Help Center",
				"slug": "wavo-help-center",
				"config": {"default_locale": "en"}
			}]
		}`))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	var out bytes.Buffer
	app.Printer.Writer = &out

	if err := (&HCDefaultCmd{Slug: "wavo-help-center"}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.HelpCenter.DefaultPortalSlug != "wavo-help-center" {
		t.Fatalf("DefaultPortalSlug = %q", loaded.HelpCenter.DefaultPortalSlug)
	}
	if loaded.HelpCenter.DefaultLocale != "en" {
		t.Fatalf("DefaultLocale = %q", loaded.HelpCenter.DefaultLocale)
	}
	if !strings.Contains(out.String(), "Default help center set to wavo-help-center (en)") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestHCsListsHelpCenters(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/portals" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"payload": [{
				"id": 1,
				"name": "Wavo Help Center",
				"slug": "wavo-help-center",
				"config": {
					"default_locale": "en",
					"allowed_locales": [{"code": "en"}, {"code": "fr"}]
				},
				"meta": {"published_count": 12, "categories_count": 3}
			}]
		}`))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	var out bytes.Buffer
	app.Printer.Writer = &out

	if err := (&HCsCmd{}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"Wavo Help Center", "wavo-help-center", "en, fr", "12", "3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestHCDefaultShowsAndClearsDefault(t *testing.T) {
	setupTestEnv(t)

	cfg := &config.Config{
		BaseURL:   "https://example.test",
		AccountID: 1,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "wavo-help-center",
			DefaultLocale:     "en",
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	var showOut bytes.Buffer
	showApp := &App{Config: cfg, Printer: testPrinter(&showOut)}
	if err := (&HCDefaultCmd{}).Run(showApp); err != nil {
		t.Fatalf("show Run: %v", err)
	}
	if !strings.Contains(showOut.String(), "wavo-help-center") || !strings.Contains(showOut.String(), "en") {
		t.Fatalf("unexpected show output: %s", showOut.String())
	}

	var clearOut bytes.Buffer
	clearApp := &App{Config: cfg, Printer: testPrinter(&clearOut)}
	if err := (&HCDefaultCmd{Clear: true}).Run(clearApp); err != nil {
		t.Fatalf("clear Run: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.HelpCenter.DefaultPortalSlug != "" || loaded.HelpCenter.DefaultLocale != "" {
		t.Fatalf("help center default was not cleared: %#v", loaded.HelpCenter)
	}
}

func TestHCDefaultDoesNotMutateLoginWhileAuthLockHeld(t *testing.T) {
	setupTestEnv(t)
	t.Setenv(config.APIKeyEnv, "")
	old := &config.Config{
		BaseURL:   brand.DefaultBaseURL,
		AccountID: 7,
		UserID:    11,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "old-portal",
			DefaultLocale:     "en",
		},
	}
	if err := config.Save(old); err != nil {
		t.Fatalf("save old config: %v", err)
	}
	if err := config.SaveAPIKey(old, "old-token"); err != nil {
		t.Fatalf("save old credential: %v", err)
	}
	held, err := lock.AcquireAuth()
	if err != nil {
		t.Fatalf("AcquireAuth: %v", err)
	}
	defer held.Release()

	app := &App{Config: old, Printer: testPrinter(&bytes.Buffer{})}
	err = (&HCDefaultCmd{Clear: true}).Run(app)
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("clear error = %v, want ErrLocked", err)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *old {
		t.Fatalf("clear changed config while locked: cfg=%#v err=%v", loaded, err)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "old-token" {
		t.Fatalf("clear changed credential while locked: got=%q err=%v", got, err)
	}
}

func TestHCDefaultDoesNotOverwriteNewerLogin(t *testing.T) {
	setupTestEnv(t)
	t.Setenv(config.APIKeyEnv, "")
	old := &config.Config{
		BaseURL:   brand.DefaultBaseURL + "/",
		AccountID: 7,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "old-portal",
			DefaultLocale:     "en",
		},
	}
	if err := config.Save(old); err != nil {
		t.Fatalf("save old config: %v", err)
	}
	if err := config.SaveAPIKey(old, "old-token"); err != nil {
		t.Fatalf("save old credential: %v", err)
	}
	app := &App{Config: old, Printer: testPrinter(&bytes.Buffer{})}

	newer := &config.Config{
		BaseURL:   brand.DefaultBaseURL,
		AccountID: 7,
		UserID:    77,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "new-portal",
			DefaultLocale:     "it",
		},
	}
	if err := config.SaveAPIKey(newer, "new-token"); err != nil {
		t.Fatalf("save newer credential: %v", err)
	}
	if err := config.Save(newer); err != nil {
		t.Fatalf("save newer config: %v", err)
	}

	err := (&HCDefaultCmd{Clear: true}).Run(app)
	if err == nil || !strings.Contains(err.Error(), "Wavo") || !strings.Contains(strings.ToLower(err.Error()), "retry") {
		t.Fatalf("clear error = %v, want actionable stale-auth error", err)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *newer {
		t.Fatalf("stale clear overwrote newer config: cfg=%#v err=%v", loaded, err)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "new-token" {
		t.Fatalf("newer keyring/config do not correspond: got=%q err=%v", got, err)
	}
	if app.Config != old {
		t.Fatalf("stale command replaced app config on failure: %#v", app.Config)
	}
}

func TestHCArticlesRendersTextTable(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hc/wavo-help-center/en/articles.json" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"payload": [{
				"id": 123,
				"title": "Create an account",
				"slug": "create-account",
				"description": "Short setup guide",
				"category": {"id": 4, "slug": "accounts", "locale": "en"},
				"link": "hc/wavo-help-center/articles/create-account"
			}],
			"meta": {"articles_count": 1}
		}`))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{
		BaseURL:   server.URL,
		AccountID: 1,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "wavo-help-center",
			DefaultLocale:     "en",
		},
	}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	var out bytes.Buffer
	app.Printer.Writer = &out

	if err := (&HCArticlesCmd{}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"123", "Create an account", "accounts", "create-account", "Short setup guide"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestHCArticlesUsesConfiguredDefaults(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hc/wavo-help-center/en/articles.json" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("query"); got != "account" {
			t.Errorf("query = %q, want account", got)
		}
		if r.URL.Query().Has("sort") {
			t.Errorf("sort should not be sent: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"payload": [{"id": 123, "title": "Create an account", "slug": "create-account", "content": "Account setup"}],
			"meta": {"articles_count": 1}
		}`))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{
		BaseURL:   server.URL,
		AccountID: 1,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "wavo-help-center",
			DefaultLocale:     "en",
		},
	}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "json"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	var out bytes.Buffer
	app.Printer.Writer = &out

	if err := (&HCArticlesCmd{Query: "account"}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got struct {
		Payload []struct {
			ID int `json:"id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if len(got.Payload) != 1 || got.Payload[0].ID != 123 {
		t.Fatalf("unexpected output: %#v", got)
	}
}

func TestHCArticlesOverridesConfiguredDefaults(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hc/other/fr/categories/getting-started/articles.json" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Errorf("page = %q, want 2", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "10" {
			t.Errorf("per_page = %q, want 10", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payload": [], "meta": {"articles_count": 0}}`))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{
		BaseURL:   server.URL,
		AccountID: 1,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "wavo-help-center",
			DefaultLocale:     "en",
		},
	}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.Printer.Writer = &bytes.Buffer{}

	if err := (&HCArticlesCmd{
		PortalSlug:   "other",
		Locale:       "fr",
		CategorySlug: "getting-started",
		Page:         2,
		PerPage:      10,
	}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestHCArticleUsesConfiguredPortal(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hc/wavo-help-center/articles/create-account.json" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 123, "title": "Create an account", "slug": "create-account", "content": "Full body"}`))
	}))
	t.Cleanup(server.Close)

	if err := config.Save(&config.Config{
		BaseURL:   server.URL,
		AccountID: 1,
		HelpCenter: config.HelpCenterConfig{
			DefaultPortalSlug: "wavo-help-center",
			DefaultLocale:     "en",
		},
	}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "json"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.Printer.Writer = &bytes.Buffer{}

	if err := (&HCArticleCmd{ArticleSlug: "create-account"}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestHCArticlesErrorsWithoutDefaultPortal(t *testing.T) {
	var out bytes.Buffer
	err := (&HCArticlesCmd{}).Run(&App{
		Config:  &config.Config{},
		Printer: testPrinter(&out),
	})
	if err == nil {
		t.Fatal("expected missing default error")
	}
	if !strings.Contains(err.Error(), "wavo hc default <slug>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testPrinter(out *bytes.Buffer) *output.Printer {
	printer := output.NewPrinter("text", false, false)
	printer.Writer = out
	return printer
}
