package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/config"
	"github.com/iRaxe/wavo-cli/internal/lock"
	"github.com/iRaxe/wavo-cli/internal/sdk"
	"github.com/zalando/go-keyring"
)

func setupTestEnv(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	if err := keyring.DeleteAll("wavo-cli"); err != nil {
		t.Fatalf("keyring.DeleteAll: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "test-token")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAssignMeWithAccountOverrideDoesNotPersistOverride(t *testing.T) {
	setupTestEnv(t)

	var sawAssignment bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/profile":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":99,"name":"Ada Lovelace","email":"ada@example.com"}`))
		case "/api/v1/accounts/2/conversations/123/assignments":
			sawAssignment = true
			var body struct {
				AssigneeID int `json:"assignee_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode assignment body: %v", err)
			}
			if body.AssigneeID != 99 {
				t.Errorf("assignee_id = %d, want 99", body.AssigneeID)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":99,"account_id":2,"name":"Ada Lovelace"}`))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	app, err := NewApp(&CLI{Output: "text", Account: 2}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if app.Client.AccountID != 2 {
		t.Fatalf("client account = %d, want override account 2", app.Client.AccountID)
	}

	if err := (&ConvAssignCmd{ID: 123, Agent: "me"}).Run(app); err != nil {
		t.Fatalf("ConvAssignCmd.Run: %v", err)
	}
	if !sawAssignment {
		t.Fatal("assignment endpoint was not called")
	}

	post, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if post.AccountID != 1 {
		t.Fatalf("persisted account_id = %d, want original account 1", post.AccountID)
	}
	if post.UserID != 0 {
		t.Fatalf("persisted user_id = %d, want unchanged user 0 for environment credential", post.UserID)
	}
}

func TestResolveAgentMeEnvironmentCredentialIgnoresCachedPersistedUser(t *testing.T) {
	setupTestEnv(t)
	t.Setenv(config.APIKeyEnv, "environment-user-b-token")
	profileCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profileCalled = true
		if got := r.Header.Get("api_access_token"); got != "environment-user-b-token" {
			t.Errorf("profile token = %q, want environment token", got)
		}
		_, _ = io.WriteString(w, `{"id":202,"name":"Environment User B"}`)
	}))
	defer server.Close()
	persisted := &config.Config{BaseURL: server.URL, AccountID: 1, UserID: 101}
	if err := config.Save(persisted); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := config.SaveAPIKey(persisted, "persisted-user-a-token"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	id, err := resolveAgent(app, "me")
	if err != nil || id != 202 || !profileCalled {
		t.Fatalf("resolveAgent = (%d, %v), profileCalled=%v; want environment user 202", id, err, profileCalled)
	}
	if app.Config.UserID != 101 {
		t.Fatalf("app cached UserID = %d, want persisted user A 101", app.Config.UserID)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || loaded.UserID != 101 {
		t.Fatalf("persisted UserID changed: cfg=%#v err=%v", loaded, err)
	}
	t.Setenv(config.APIKeyEnv, "")
	if got, source, err := config.ResolveAPIKey(loaded); err != nil || got != "persisted-user-a-token" || source != config.CredentialSourceKeyring {
		t.Fatalf("persisted credential changed: got=%q source=%q err=%v", got, source, err)
	}
}

func TestResolveAgentMeEnvironmentCredentialDoesNotCacheProfileUser(t *testing.T) {
	setupTestEnv(t)
	t.Setenv(config.APIKeyEnv, "environment-user-b-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":202,"name":"Environment User B"}`)
	}))
	defer server.Close()
	persisted := &config.Config{BaseURL: server.URL, AccountID: 1}
	if err := config.Save(persisted); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := config.SaveAPIKey(persisted, "persisted-token"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	id, err := resolveAgent(app, "me")
	if err != nil || id != 202 {
		t.Fatalf("resolveAgent = (%d, %v), want environment user 202", id, err)
	}
	if app.Config.UserID != 0 {
		t.Fatalf("app cached UserID = %d, want 0", app.Config.UserID)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || loaded.UserID != 0 {
		t.Fatalf("persisted UserID changed: cfg=%#v err=%v", loaded, err)
	}
}

func TestResolveAgentMeProfileErrorsAreGenericAndDoNotPersist(t *testing.T) {
	const token = "active token+API/error"
	for _, test := range []struct {
		name   string
		source config.CredentialSource
		userID int
	}{
		{name: "environment cached user", source: config.CredentialSourceEnvironment, userID: 101},
		{name: "environment empty cache", source: config.CredentialSourceEnvironment},
		{name: "keyring empty cache", source: config.CredentialSourceKeyring},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTestEnv(t)
			body := "provider diagnostic raw=" + token + " query=" + url.QueryEscape(token) + " path=" + url.PathEscape(token)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, body, http.StatusUnauthorized)
			}))
			defer server.Close()
			persisted := &config.Config{BaseURL: server.URL, AccountID: 1, UserID: test.userID}
			if err := config.Save(persisted); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if err := config.SaveAPIKey(persisted, token); err != nil {
				t.Fatalf("SaveAPIKey: %v", err)
			}
			if test.source == config.CredentialSourceEnvironment {
				t.Setenv(config.APIKeyEnv, token)
			} else {
				t.Setenv(config.APIKeyEnv, "")
			}
			app, err := NewApp(&CLI{Output: "text"}, false, "test")
			if err != nil {
				t.Fatalf("NewApp: %v", err)
			}

			id, err := resolveAgent(app, "me")
			if id != 0 || err != errResolveAgentProfile {
				t.Fatalf("resolveAgent = (%d, %v), want constant Wavo profile error", id, err)
			}
			for _, leaked := range []string{token, url.QueryEscape(token), url.PathEscape(token), body, "provider diagnostic", server.URL} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("resolveAgent error exposed %q: %q", leaked, err)
				}
			}
			loaded, loadErr := config.Load()
			if loadErr != nil || loaded == nil || *loaded != *persisted {
				t.Fatalf("profile error changed config: cfg=%#v err=%v", loaded, loadErr)
			}
			if app.Config.UserID != test.userID {
				t.Fatalf("profile error changed app UserID = %d, want %d", app.Config.UserID, test.userID)
			}
		})
	}
}

func TestResolveAgentMePersistedSourcesUseCachedUser(t *testing.T) {
	for _, test := range []struct {
		name   string
		source config.CredentialSource
	}{
		{name: "zero source", source: ""},
		{name: "keyring", source: config.CredentialSourceKeyring},
	} {
		t.Run(test.name, func(t *testing.T) {
			profileCalled := false
			httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				profileCalled = true
				return nil, errors.New("profile should not be requested")
			})}
			app := &App{
				Config:           &config.Config{BaseURL: "https://wavo.example", AccountID: 7, UserID: 101},
				Client:           sdk.NewClient("https://wavo.example", "persisted-token", 7, sdk.WithHTTPClient(httpClient)),
				CredentialSource: test.source,
			}

			id, err := resolveAgent(app, "me")
			if err != nil || id != 101 || profileCalled {
				t.Fatalf("resolveAgent = (%d, %v), profileCalled=%v; want cached user 101", id, err, profileCalled)
			}
		})
	}
}

func TestResolveAgentMeDoesNotMutateWhileAuthLockHeld(t *testing.T) {
	setupTestEnv(t)
	t.Setenv(config.APIKeyEnv, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/profile" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"id":99,"name":"Profile User"}`)
	}))
	defer server.Close()
	old := &config.Config{BaseURL: server.URL, AccountID: 1}
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
	app := &App{Config: old, Client: sdk.NewClient(server.URL, "old-token", 1)}

	id, err := resolveAgent(app, "me")
	if id != 0 || !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("resolveAgent = (%d, %v), want 0/ErrLocked", id, err)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *old {
		t.Fatalf("resolveAgent changed config while locked: cfg=%#v err=%v", loaded, err)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "old-token" {
		t.Fatalf("resolveAgent changed credential while locked: got=%q err=%v", got, err)
	}
}

func TestResolveAgentMeDoesNotOverwriteNewerLogin(t *testing.T) {
	setupTestEnv(t)
	t.Setenv(config.APIKeyEnv, "")
	old := &config.Config{BaseURL: "https://same.wavo.example", AccountID: 1}
	if err := config.Save(old); err != nil {
		t.Fatalf("save old config: %v", err)
	}
	if err := config.SaveAPIKey(old, "old-token"); err != nil {
		t.Fatalf("save old credential: %v", err)
	}
	newer := &config.Config{BaseURL: old.BaseURL, AccountID: old.AccountID, UserID: 77}
	var app *App
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		held, err := lock.AcquireAuth()
		if err == nil {
			err = config.SaveAPIKey(newer, "new-token")
			if err == nil {
				err = config.Save(newer)
			}
			held.Release()
		}
		if err != nil {
			return nil, err
		}
		app.Config = newer
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":99,"name":"Old Profile User"}`)),
			Request:    request,
		}, nil
	})}
	app = &App{Config: old, Client: sdk.NewClient(old.BaseURL, "old-token", 1, sdk.WithHTTPClient(httpClient))}

	id, err := resolveAgent(app, "me")
	if id != 0 || err == nil || !strings.Contains(err.Error(), "Wavo") || !strings.Contains(strings.ToLower(err.Error()), "retry") {
		t.Fatalf("resolveAgent = (%d, %v), want actionable stale-auth failure", id, err)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *newer {
		t.Fatalf("resolveAgent overwrote newer config: cfg=%#v err=%v want=%#v", loaded, err, newer)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "new-token" {
		t.Fatalf("newer keyring/config do not correspond: got=%q err=%v", got, err)
	}
	if app.Config != newer || app.Config.UserID != 77 {
		t.Fatalf("resolveAgent overwrote app config for newer login: %#v", app.Config)
	}
}

func TestConvContactFetchesSenderContact(t *testing.T) {
	setupTestEnv(t)

	var sawContactGet bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/1/conversations/123":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":123,"meta":{"sender":{"id":456,"name":"Grace Hopper"}}}`))
		case "/api/v1/accounts/1/contacts/456":
			sawContactGet = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"id":456,"name":"Grace Hopper","email":"grace@example.com","phone_number":"+15555550101"}}`))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	if err := (&ConvContactCmd{ID: 123}).Run(app); err != nil {
		t.Fatalf("ConvContactCmd.Run: %v", err)
	}
	if !sawContactGet {
		t.Fatal("contact endpoint was not called — sender ID not resolved from conversation")
	}
}

func TestConvContactErrorsWhenNoSender(t *testing.T) {
	setupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/accounts/1/conversations/777" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":777,"meta":{}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/contacts/") {
			t.Errorf("contacts endpoint should not be called when sender is missing: %s", r.URL.Path)
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	defer server.Close()

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	err = (&ConvContactCmd{ID: 777}).Run(app)
	if err == nil {
		t.Fatal("expected error for conversation with no sender, got nil")
	}
	if !strings.Contains(err.Error(), "no associated contact") {
		t.Fatalf("error %q should mention missing contact", err.Error())
	}
}

type statusRunner func(app *App, id int) error

func TestConvStatusVerbsCallToggleStatus(t *testing.T) {
	cases := []struct {
		name       string
		wantStatus string
		run        statusRunner
	}{
		{"resolve", "resolved", func(a *App, id int) error { return (&ConvResolveCmd{ID: id}).Run(a) }},
		{"open", "open", func(a *App, id int) error { return (&ConvOpenCmd{ID: id}).Run(a) }},
		{"pending", "pending", func(a *App, id int) error { return (&ConvPendingCmd{ID: id}).Run(a) }},
		{"snooze-until-next-reply", "snoozed", func(a *App, id int) error { return (&ConvSnoozeCmd{ID: id}).Run(a) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTestEnv(t)

			var got struct {
				Status       string `json:"status"`
				SnoozedUntil *int64 `json:"snoozed_until,omitempty"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/accounts/1/conversations/55/toggle_status" {
					http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"current_status":"` + tc.wantStatus + `","conversation_id":55}`))
			}))
			defer server.Close()

			if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
				t.Fatalf("config.Save: %v", err)
			}
			app, err := NewApp(&CLI{Output: "text"}, false, "test")
			if err != nil {
				t.Fatalf("NewApp: %v", err)
			}

			if err := tc.run(app, 55); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("posted status = %q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}

func TestConvUnassignPostsZeroAssignee(t *testing.T) {
	setupTestEnv(t)

	var got struct {
		AssigneeID *int `json:"assignee_id"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/conversations/42/assignments" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 1}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app, err := NewApp(&CLI{Output: "text"}, false, "test")
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	if err := (&ConvUnassignCmd{ID: 42}).Run(app); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.AssigneeID == nil || *got.AssigneeID != 0 {
		t.Errorf("posted assignee_id = %v, want 0 (Wavo's unassign sentinel)", got.AssigneeID)
	}
}
