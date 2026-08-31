package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/iRaxe/wavo-cli/internal/config"
	"github.com/iRaxe/wavo-cli/internal/lock"
	"github.com/iRaxe/wavo-cli/internal/output"
	"github.com/iRaxe/wavo-cli/internal/sdk"
	"github.com/zalando/go-keyring"
)

// setupAuthStatusEnv isolates HOME and the keyring, stubs the Profile
// endpoint, saves a config pointing at the stub, and authenticates via the
// WAVO_API_KEY env override (so source = "environment").
//
// Returns a cleanup function that closes the stub server.
func setupAuthStatusEnv(t *testing.T, profileBody string) func() {
	t.Helper()
	keyring.MockInit()
	if err := keyring.DeleteAll(brand.KeyringService); err != nil {
		t.Fatalf("keyring.DeleteAll: %v", err)
	}
	t.Setenv("HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/profile" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(profileBody))
	}))

	cfg := &config.Config{BaseURL: server.URL, AccountID: 1}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	t.Setenv(config.APIKeyEnv, "test-token")

	return server.Close
}

func setupAuthStatusKeyring(t *testing.T, profileBody string) func() {
	t.Helper()
	keyring.MockInit()
	if err := keyring.DeleteAll(brand.KeyringService); err != nil {
		t.Fatalf("keyring.DeleteAll: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/profile" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(profileBody))
	}))

	cfg := &config.Config{BaseURL: server.URL, AccountID: 1}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	if err := config.SaveAPIKey(cfg, "test-token"); err != nil {
		t.Fatalf("config.SaveAPIKey: %v", err)
	}

	return server.Close
}

// runAndCapture runs a Cmd's Run with a buffered Printer and returns stdout.
func runAndCapture(t *testing.T, run func(*App) error) string {
	t.Helper()
	var out bytes.Buffer
	printer := output.NewPrinter("text", false, false)
	printer.Writer = &out
	if err := run(&App{Printer: printer}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String()
}

func TestAuthStatusReportsIdentityAndCredentialSource(t *testing.T) {
	profile := `{
		"id": 42,
		"name": "Ada Lovelace",
		"email": "ada@example.com",
		"role": "agent",
		"availability_status": "online"
	}`
	defer setupAuthStatusEnv(t, profile)()

	got := runAndCapture(t, (&AuthStatusCmd{}).Run)

	wants := []string{
		"Ada Lovelace",
		"ada@example.com",
		"agent",
		"online",
		"User ID:", "42",
		"Workspace:", "1",
		"Credential:", "environment",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestAuthStatusJSONPreservesAccountAndAddsSanitizedWorkspace(t *testing.T) {
	profile := `{
		"id": 42,
		"name": "Ada Lovelace",
		"email": "ada@example.com",
		"accounts": [{"id": 1, "name": "Acme\u001b[31m", "role": "administrator"}]
	}`
	defer setupAuthStatusEnv(t, profile)()

	var out bytes.Buffer
	printer := output.NewPrinter("json", false, false)
	printer.Writer = &out
	if err := (&AuthStatusCmd{}).Run(&App{Printer: printer}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, out.String())
	}
	if got["Account"] != "1" {
		t.Fatalf("Account = %q, want numeric string 1; JSON=%s", got["Account"], out.String())
	}
	if got["Workspace"] != "1 (Acme)" {
		t.Fatalf("Workspace = %q, want sanitized workspace; JSON=%s", got["Workspace"], out.String())
	}
	if strings.Contains(got["Workspace"], "\x1b") {
		t.Fatalf("Workspace contains terminal control: %q", got["Workspace"])
	}
}

func TestAuthStatusRedactsCredentialReflectedBySuccessfulProfile(t *testing.T) {
	const token = "a b+c/API:error"
	profileBody := reflectedProfileJSON(t, token)
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, token)
	server := loginProfileServer(t, profileBody)
	defer server.Close()
	if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 9}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	var out bytes.Buffer
	printer := output.NewPrinter("json", false, false)
	printer.Writer = &out
	if err := (&AuthStatusCmd{}).Run(&App{Printer: printer}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertNoCredentialReflection(t, out.String(), token)
	for _, safe := range []string{"Name [REDACTED] Tail", `"Email": "[REDACTED]"`, `"Role": "[REDACTED]"`, `"Availability": "[REDACTED]"`, "9 ([REDACTED])"} {
		if !strings.Contains(out.String(), safe) {
			t.Fatalf("status output did not retain safe text %q: %s", safe, out.String())
		}
	}
}

func TestAuthStatusDoesNotExposeProviderErrorBodyOrToken(t *testing.T) {
	for _, token := range []string{"API", "error", ":", "a b+c"} {
		for _, mode := range []string{"body", "redirect"} {
			t.Run(token+"/"+mode, func(t *testing.T) {
				server, targetURL, location := authFailureServer(t, mode, token)
				keyring.MockInit()
				t.Setenv("HOME", t.TempDir())
				t.Setenv(config.APIKeyEnv, token)
				if err := config.Save(&config.Config{BaseURL: server.URL, AccountID: 7}); err != nil {
					t.Fatalf("config.Save: %v", err)
				}
				printer := output.NewPrinter("text", false, false)
				printer.Writer = io.Discard
				err := (&AuthStatusCmd{}).Run(&App{Printer: printer})
				if err == nil {
					t.Fatal("auth status unexpectedly succeeded")
				}
				assertGenericAuthError(t, err, token, server.URL, targetURL, location)
			})
		}
	}
}

func TestNormalizeLoginBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		allowHTTP bool
		want      string
		wantErr   string
	}{
		{name: "default", input: "", want: brand.DefaultBaseURL},
		{name: "trims whitespace and trailing slash", input: "  https://wavo.example/  ", want: "https://wavo.example"},
		{name: "allows HTTP in development", input: "http://127.0.0.1:3000/", allowHTTP: true, want: "http://127.0.0.1:3000"},
		{name: "rejects HTTP in production", input: "http://wavo.example", wantErr: "HTTPS"},
		{name: "rejects user info", input: "https://user:pass@wavo.example", wantErr: "only scheme and host"},
		{name: "rejects query", input: "https://wavo.example?token=secret", wantErr: "only scheme and host"},
		{name: "rejects empty query marker", input: "https://wavo.example?", wantErr: "only scheme and host"},
		{name: "rejects fragment", input: "https://wavo.example/#settings", wantErr: "only scheme and host"},
		{name: "rejects empty fragment marker", input: "https://wavo.example#", wantErr: "only scheme and host"},
		{name: "rejects path", input: "https://wavo.example/api", wantErr: "only scheme and host"},
		{name: "rejects schemeless host", input: "wavo.example", wantErr: "only scheme and host"},
		{name: "rejects empty hostname", input: "https://:443", wantErr: "only scheme and host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeLoginBaseURL(tt.input, tt.allowHTTP)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("normalizeLoginBaseURL() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeLoginBaseURL() = (%q, %v), want (%q, nil)", got, err, tt.want)
			}
		})
	}
}

func TestSelectedAccountOmitsEmptySanitizedName(t *testing.T) {
	accounts := []profileDisplayAccount{{ID: 7, Name: "\x1b[31m"}}
	if got := selectedAccount(accounts, 7); got != "7" {
		t.Fatalf("selectedAccount() = %q, want numeric ID only", got)
	}
}

func TestAuthProfileErrorsAreConstant(t *testing.T) {
	if got, want := errLoginProfile.Error(), "Wavo authentication failed. Verify credentials or run 'wavo auth login'."; got != want {
		t.Fatalf("login profile error = %q, want %q", got, want)
	}
	if got, want := errStatusProfile.Error(), "Wavo profile unavailable. Check availability or run 'wavo auth login'."; got != want {
		t.Fatalf("status profile error = %q, want %q", got, want)
	}
}

func TestRedactAuthError(t *testing.T) {
	token := "secret /?+value"
	message := "request failed | " + strings.Join(credentialVariants(token), " | ")

	got := redactAuthError(message, token)
	if got != "[REDACTED]" {
		t.Fatalf("redactAuthError() = %q, want fail-closed marker", got)
	}
	if got := redactAuthError("unchanged", ""); got != "unchanged" {
		t.Fatalf("redactAuthError() with empty token = %q, want unchanged", got)
	}
}

func TestSafeProfileForDisplayRedactsCredentialWithoutMutatingProfile(t *testing.T) {
	const token = "a b+c/API:error"
	profile := reflectedProfile(t, token)
	safe := safeProfileForDisplay(profile, token)

	if !strings.Contains(profile.Name, token) {
		t.Fatalf("safeProfileForDisplay mutated raw profile: %#v", profile)
	}
	if len(safe.Accounts) != len(profile.Accounts) || safe.Accounts[1].ID != profile.Accounts[1].ID {
		t.Fatalf("safe profile changed membership IDs: raw=%#v safe=%#v", profile.Accounts, safe.Accounts)
	}
	for _, displayed := range []string{
		safe.Name,
		safe.Email,
		safe.Role,
		safe.AvailabilityStatus,
		safe.Accounts[0].Name,
		safe.Accounts[0].Role,
		safe.Accounts[1].Name,
		safe.Accounts[1].Role,
	} {
		assertNoCredentialReflection(t, displayed, token)
	}
}

func TestSafeProfileTextRedactsLiteralPercentCredential(t *testing.T) {
	const token = "key%41value"
	got := safeProfileText("before "+token+" after", token)
	if strings.Contains(got, token) || got != "before [REDACTED] after" {
		t.Fatalf("safeProfileText() = %q, want literal percent credential removed", got)
	}
}

func TestSafeProfileTextFailsClosedOnEncodingMarkers(t *testing.T) {
	const (
		token = "a b+c"
		field = "100% a%2Bb%252Bc"
	)
	got := safeProfileText(field, token)
	if got != "[REDACTED]" {
		t.Fatalf("safeProfileText() = %q, want fail-closed marker", got)
	}
	for _, leaked := range []string{token, "a%2Bb%252Bc", field} {
		if strings.Contains(got, leaked) {
			t.Fatalf("safeProfileText() exposed %q in %q", leaked, got)
		}
	}
}

func TestSafeProfileTextRetainsSafeTextAroundRawCredential(t *testing.T) {
	const token = "plain-secret"
	if got := safeProfileText("before "+token+" after", token); got != "before [REDACTED] after" {
		t.Fatalf("safeProfileText() = %q, want safe surrounding text", got)
	}
}

// Encoding markers are conservatively treated as credential carriers. This
// intentionally hides legitimate percent/plus text rather than risk disclosure.
func TestSafeProfileTextConservativelyRedactsLegitimateEncodingMarkers(t *testing.T) {
	const token = "unrelated-secret"
	for _, field := range []string{"Discount 100% available", "Research + Development"} {
		if got := safeProfileText(field, token); got != "[REDACTED]" {
			t.Fatalf("safeProfileText(%q) = %q, want fail-closed marker", field, got)
		}
	}
}

func TestSelectAccount(t *testing.T) {
	accounts := []profileDisplayAccount{
		{ID: 7, Name: "Acme", Role: "administrator"},
		{ID: 9, Name: "Beta\x1b[31m", Role: "agent\x1b]52;c;Zm9v\a"},
	}

	var out bytes.Buffer
	got, err := selectAccount(strings.NewReader("2\n"), accounts, 0, &out)
	if err != nil || got != 9 {
		t.Fatalf("selectAccount() = (%d, %v), want (9, nil)", got, err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("workspace menu contains terminal control: %q", out.String())
	}

	if _, err := selectAccount(strings.NewReader("3\n"), accounts, 0, io.Discard); err == nil {
		t.Fatal("selectAccount() accepted out-of-range selection")
	}
	if got, err := selectAccount(strings.NewReader(""), accounts[:1], 0, io.Discard); err != nil || got != 7 {
		t.Fatalf("single-account selectAccount() = (%d, %v), want (7, nil)", got, err)
	}
	if got, err := selectAccount(strings.NewReader(""), nil, 11, io.Discard); err != nil || got != 11 {
		t.Fatalf("fallback selectAccount() = (%d, %v), want (11, nil)", got, err)
	}
}

func TestAuthLoginRequiresTerminal(t *testing.T) {
	isolateAuthEnv(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := io.WriteString(w, "must-not-be-read\n"); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	err = (&AuthLoginCmd{}).Run(&App{})
	if err == nil || !strings.Contains(err.Error(), config.APIKeyEnv) {
		t.Fatalf("Run() error = %v, want headless guidance containing %s", err, config.APIKeyEnv)
	}
	remaining, readErr := io.ReadAll(r)
	if readErr != nil || string(remaining) != "must-not-be-read\n" {
		t.Fatalf("Run() consumed non-terminal stdin: remaining=%q err=%v", remaining, readErr)
	}
	if cfg, err := config.Load(); err != nil || cfg != nil {
		t.Fatalf("Run() saved config in non-terminal mode: cfg=%#v err=%v", cfg, err)
	}
	seed := &config.Config{BaseURL: brand.DefaultBaseURL, AccountID: 7}
	if _, _, err := config.ResolveAPIKey(seed); !errors.Is(err, config.ErrAPIKeyNotFound) {
		t.Fatalf("Run() saved a token in non-terminal mode: %v", err)
	}
}

func TestLoginSuccessMessageStripsTerminalControls(t *testing.T) {
	got := loginSuccessMessage("Eve\x1b]52;c;Zm9v\a", "eve@example.com\x1b[31m")
	for _, disallowed := range []string{"\x1b", "\a", "]52", "[31m"} {
		if strings.Contains(got, disallowed) {
			t.Fatalf("login success message contained terminal control %q: %q", disallowed, got)
		}
	}
	if !strings.Contains(got, "Logged in as Eve (eve@example.com)") {
		t.Fatalf("login success message stripped printable content: %q", got)
	}
}

func TestVerifyAccountAccess(t *testing.T) {
	accounts := []sdk.ProfileAccount{
		{ID: 7, Name: "Acme", Role: "administrator"},
		{ID: 9, Name: "Beta", Role: "agent"},
	}

	if err := verifyAccountAccess(&sdk.ProfileResponse{Accounts: accounts}, 9); err != nil {
		t.Fatalf("expected access to a member account, got error: %v", err)
	}

	err := verifyAccountAccess(&sdk.ProfileResponse{Accounts: accounts}, 42)
	if err == nil {
		t.Fatal("expected error for non-member account, got nil")
	}
	// The message should name the accessible accounts so the user can correct the ID.
	for _, want := range []string{"42", "7 (Acme)", "9 (Beta)"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}

	// Older instances may omit accounts and provide one account_id fallback.
	if err := verifyAccountAccess(&sdk.ProfileResponse{AccountID: 42}, 42); err != nil {
		t.Fatalf("expected matching fallback account, got error: %v", err)
	}
	if err := verifyAccountAccess(&sdk.ProfileResponse{AccountID: 42}, 7); err == nil {
		t.Fatal("expected mismatched fallback account to be rejected")
	}
	if err := verifyAccountAccess(&sdk.ProfileResponse{}, 0); err == nil {
		t.Fatal("expected non-positive workspace ID to be rejected")
	}
}

func TestMeAndWhoamiAliasAuthStatus(t *testing.T) {
	profile := `{
		"id": 7,
		"name": "Grace Hopper",
		"email": "grace@example.com",
		"role": "administrator",
		"availability_status": "offline"
	}`
	defer setupAuthStatusEnv(t, profile)()

	authOut := runAndCapture(t, (&AuthStatusCmd{}).Run)
	meOut := runAndCapture(t, (&MeCmd{}).Run)
	whoamiOut := runAndCapture(t, (&WhoamiCmd{}).Run)

	if authOut != meOut {
		t.Fatalf("'me' diverged from 'auth status':\nauth status:\n%s\nme:\n%s", authOut, meOut)
	}
	if authOut != whoamiOut {
		t.Fatalf("'whoami' diverged from 'auth status':\nauth status:\n%s\nwhoami:\n%s", authOut, whoamiOut)
	}
}

func TestAuthStatusNotLoggedIn(t *testing.T) {
	keyring.MockInit()
	if err := keyring.DeleteAll(brand.KeyringService); err != nil {
		t.Fatalf("keyring.DeleteAll: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	// no config saved → IsValid() returns false

	got := runAndCapture(t, (&AuthStatusCmd{}).Run)
	if !strings.Contains(got, "Not logged in") {
		t.Fatalf("expected 'Not logged in', got: %s", got)
	}
	if !strings.Contains(got, "wavo auth login") {
		t.Fatalf("expected the message to point at 'wavo auth login', got: %s", got)
	}
}

func TestAuthLogoutRemovesKeyringTokenWithoutConfig(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "")

	// Seed the token through the production path so it lands under whichever
	// keyring service the active build profile uses (prod vs dev), without
	// writing config.yaml — this exercises logout with no config present.
	seed := &config.Config{BaseURL: brand.DefaultBaseURL, AccountID: 1}
	if err := config.SaveAPIKey(seed, "stale-token"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}

	if err := (&AuthLogoutCmd{}).Run(&App{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, _, err := config.ResolveAPIKey(seed); !errors.Is(err, config.ErrAPIKeyNotFound) {
		t.Fatalf("expected logout to delete the keyring token, err = %v", err)
	}
}

func TestAuthLogoutRemovesCredentialWhenConfigIsMalformed(t *testing.T) {
	isolateAuthEnv(t)
	seed := &config.Config{BaseURL: brand.DefaultBaseURL, AccountID: 7}
	if err := config.SaveAPIKey(seed, "stale-token"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	path, err := config.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("base_url: [malformed\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := (&AuthLogoutCmd{}).Run(&App{}); err != nil {
		t.Fatalf("logout malformed config: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed config still exists: %v", err)
	}
	if _, _, err := config.ResolveAPIKey(seed); !errors.Is(err, config.ErrAPIKeyNotFound) {
		t.Fatalf("credential remained after malformed-config logout: %v", err)
	}
}

func TestAuthLogoutDoesNotMutateWhileAuthLockHeld(t *testing.T) {
	isolateAuthEnv(t)
	cfg := &config.Config{BaseURL: brand.DefaultBaseURL, AccountID: 7, UserID: 11}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	if err := config.SaveAPIKey(cfg, "stable-token"); err != nil {
		t.Fatalf("config.SaveAPIKey: %v", err)
	}
	held, err := lock.AcquireAuth()
	if err != nil {
		t.Fatalf("AcquireAuth: %v", err)
	}
	defer held.Release()

	err = (&AuthLogoutCmd{}).Run(&App{})
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("logout error = %v, want ErrLocked", err)
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *cfg {
		t.Fatalf("logout changed config while locked: cfg=%#v err=%v", loaded, err)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "stable-token" {
		t.Fatalf("logout changed credential while locked: got=%q err=%v", got, err)
	}
}

func TestAuthStatusSelfHealsCachedUserID(t *testing.T) {
	profile := `{
		"id": 99,
		"name": "Alan Turing",
		"email": "alan@example.com",
		"role": "agent",
		"availability_status": "online"
	}`
	defer setupAuthStatusKeyring(t, profile)()

	pre, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load (pre): %v", err)
	}
	if pre.UserID != 0 {
		t.Fatalf("setup: expected UserID=0, got %d", pre.UserID)
	}

	_ = runAndCapture(t, (&AuthStatusCmd{}).Run)

	post, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load (post): %v", err)
	}
	if post.UserID != 99 {
		t.Fatalf("expected UserID=99 cached after auth status, got %d", post.UserID)
	}
}

func TestAuthStatusDoesNotCacheUserIDFromEnvironmentToken(t *testing.T) {
	profile := `{
		"id": 99,
		"name": "Alan Turing",
		"email": "alan@example.com",
		"role": "agent",
		"availability_status": "online"
	}`
	defer setupAuthStatusEnv(t, profile)()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load (pre): %v", err)
	}
	cfg.UserID = 42
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	_ = runAndCapture(t, (&AuthStatusCmd{}).Run)

	post, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load (post): %v", err)
	}
	if post.UserID != 42 {
		t.Fatalf("expected env-token auth status to preserve cached UserID=42, got %d", post.UserID)
	}
}

func TestAuthStatusSelfHealDoesNotOverwriteNewerLogin(t *testing.T) {
	isolateAuthEnv(t)
	updateResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		held, err := lock.AcquireAuth()
		if err == nil {
			newer := &config.Config{BaseURL: "http://" + r.Host, AccountID: 1, UserID: 77}
			err = config.SaveAPIKey(newer, "new-token")
			if err == nil {
				err = config.Save(newer)
			}
			held.Release()
		}
		updateResult <- err
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":99,"name":"Old User","email":"old@example.com","account_id":1}`)
	}))
	defer server.Close()
	old := &config.Config{BaseURL: server.URL, AccountID: 1, UserID: 5}
	if err := config.Save(old); err != nil {
		t.Fatalf("save old config: %v", err)
	}
	if err := config.SaveAPIKey(old, "old-token"); err != nil {
		t.Fatalf("save old credential: %v", err)
	}

	printer := output.NewPrinter("text", false, false)
	printer.Writer = io.Discard
	err := (&AuthStatusCmd{}).Run(&App{Printer: printer})
	if err == nil || !strings.Contains(err.Error(), "Wavo") || !strings.Contains(strings.ToLower(err.Error()), "retry") {
		t.Fatalf("auth status error = %v, want actionable stale-auth failure", err)
	}
	if err := <-updateResult; err != nil {
		t.Fatalf("newer login could not persist during profile request: %v", err)
	}
	loaded, err := config.Load()
	want := &config.Config{BaseURL: server.URL, AccountID: 1, UserID: 77}
	if err != nil || loaded == nil || *loaded != *want {
		t.Fatalf("self-heal overwrote newer config: cfg=%#v err=%v want=%#v", loaded, err, want)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "new-token" {
		t.Fatalf("newer keyring/config do not correspond: got=%q err=%v", got, err)
	}
}

func TestAuthLoginSelectsAndPersistsWorkspace(t *testing.T) {
	profileBody := `{"id":5,"name":"Eve","email":"eve@example.com","availability_status":"online","role":"agent",` +
		`"accounts":[{"id":7,"name":"Acme","role":"administrator"},{"id":9,"name":"Beta","role":"agent"}]}`
	server := loginProfileServer(t, profileBody)
	defer server.Close()
	isolateAuthEnv(t)

	if err := runLogin(t, server.URL+"\n2\n", "token", io.Discard); err != nil {
		t.Fatalf("login: %v", err)
	}

	cfg, err := config.Load()
	if err != nil || cfg == nil {
		t.Fatalf("config not saved: cfg=%#v err=%v", cfg, err)
	}
	if cfg.AccountID != 9 || cfg.UserID != 5 {
		t.Fatalf("saved cfg = %#v, want AccountID 9, UserID 5", cfg)
	}
	apiKey, source, err := config.ResolveAPIKey(cfg)
	if err != nil || apiKey != "token" || source != config.CredentialSourceKeyring {
		t.Fatalf("ResolveAPIKey = (%q, %v, %v), want token/keyring", apiKey, source, err)
	}
}

func TestAuthLoginDoesNotPersistWhileAuthLockHeld(t *testing.T) {
	profileCalled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profileCalled <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":5,"name":"Eve","email":"eve@example.com","accounts":[{"id":9,"name":"New","role":"administrator"}]}`)
	}))
	defer server.Close()
	isolateAuthEnv(t)
	old := &config.Config{BaseURL: brand.DefaultBaseURL, AccountID: 7, UserID: 11}
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

	err = runLogin(t, server.URL+"\n", "new-token", io.Discard)
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("login error = %v, want ErrLocked", err)
	}
	select {
	case <-profileCalled:
	default:
		t.Fatal("login acquired auth lock before profile verification")
	}
	loaded, err := config.Load()
	if err != nil || loaded == nil || *loaded != *old {
		t.Fatalf("login changed config while locked: cfg=%#v err=%v", loaded, err)
	}
	if got, _, err := config.ResolveAPIKey(loaded); err != nil || got != "old-token" {
		t.Fatalf("login changed credential while locked: got=%q err=%v", got, err)
	}
}

func TestAuthLoginRejectsInvalidURLBeforeReadingToken(t *testing.T) {
	for _, input := range []string{
		"http://wavo.example",
		"https://wavo.example?",
		"https://wavo.example#",
		"https://:443",
	} {
		t.Run(input, func(t *testing.T) {
			isolateAuthEnv(t)
			tokenRead := false
			err := (&AuthLoginCmd{}).login(
				&App{},
				strings.NewReader(input+"\n"),
				io.Discard,
				false,
				func() (string, error) {
					tokenRead = true
					return "must-not-be-read", nil
				},
			)
			if err == nil {
				t.Fatal("login() accepted invalid base URL")
			}
			if tokenRead {
				t.Fatal("login() read the token before validating the base URL")
			}
			assertNoSavedLogin(t, &config.Config{BaseURL: brand.DefaultBaseURL, AccountID: 7})
		})
	}
}

func TestAuthLoginProfileFailurePersistsNothingAndDoesNotLeakToken(t *testing.T) {
	for _, token := range []string{"API", "error", ":", "a b+c"} {
		for _, mode := range []string{"body", "redirect"} {
			t.Run(token+"/"+mode, func(t *testing.T) {
				server, targetURL, location := authFailureServer(t, mode, token)
				isolateAuthEnv(t)

				var out bytes.Buffer
				err := runLogin(t, server.URL+"\n", token, &out)
				if err == nil {
					t.Fatal("login unexpectedly succeeded")
				}
				assertGenericAuthError(t, err, token, server.URL, targetURL, location)
				for _, untrusted := range []string{"provider diagnostic", targetURL, location} {
					if strings.Contains(out.String(), untrusted) {
						t.Fatalf("login output exposed provider value %q: %q", untrusted, out.String())
					}
				}
				assertNoSavedLogin(t, &config.Config{BaseURL: server.URL, AccountID: 7})
			})
		}
	}
}

func TestAuthLoginRejectsInvalidProfileWorkspaceWithoutPersistence(t *testing.T) {
	server := loginProfileServer(t, `{"id":5,"name":"Eve","email":"eve@example.com","accounts":[{"id":0,"name":"Broken","role":"agent"}]}`)
	defer server.Close()
	isolateAuthEnv(t)

	err := runLogin(t, server.URL+"\n", "token", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("login() error = %v, want invalid workspace error", err)
	}
	assertNoSavedLogin(t, &config.Config{BaseURL: server.URL, AccountID: 1})
}

func TestAuthLoginSanitizesIdentityAndWorkspaceOutput(t *testing.T) {
	profileBody := `{"id":5,"name":"Eve\u001b]52;c;Zm9v\u0007","email":"eve@example.com\u001b[31m",` +
		`"accounts":[{"id":7,"name":"Acme","role":"administrator"},{"id":9,"name":"Beta\u001b[31m","role":"agent"}]}`
	server := loginProfileServer(t, profileBody)
	defer server.Close()
	isolateAuthEnv(t)

	var out bytes.Buffer
	if err := runLogin(t, server.URL+"\n2\n", "token", &out); err != nil {
		t.Fatalf("login: %v", err)
	}
	for _, disallowed := range []string{"\x1b", "\a", "]52", "[31m"} {
		if strings.Contains(out.String(), disallowed) {
			t.Fatalf("login output contained terminal control %q: %q", disallowed, out.String())
		}
	}
	for _, want := range []string{"Logged in as Eve (eve@example.com)", "9 (Beta)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("login output %q missing %q", out.String(), want)
		}
	}
}

func TestAuthLoginRedactsCredentialReflectedBySuccessfulProfile(t *testing.T) {
	const token = "a b+c/API:error"
	server := loginProfileServer(t, reflectedProfileJSON(t, token))
	defer server.Close()
	isolateAuthEnv(t)

	var out bytes.Buffer
	if err := runLogin(t, server.URL+"\n2\n", token, &out); err != nil {
		t.Fatalf("login: %v", err)
	}
	assertNoCredentialReflection(t, out.String(), token)
	for _, safe := range []string{"Name [REDACTED] Tail", "9 ([REDACTED])"} {
		if !strings.Contains(out.String(), safe) {
			t.Fatalf("login output did not retain safe text %q: %s", safe, out.String())
		}
	}

	cfg, err := config.Load()
	if err != nil || cfg == nil || cfg.AccountID != 9 {
		t.Fatalf("selected raw membership was not persisted: cfg=%#v err=%v", cfg, err)
	}
}

func TestAuthLoginConfigFailureRollsBackWavoCredentialOnly(t *testing.T) {
	server := loginProfileServer(t, `{"id":5,"name":"Eve","email":"eve@example.com","accounts":[{"id":7,"name":"Acme","role":"administrator"}]}`)
	defer server.Close()
	keyring.MockInit()
	if err := keyring.DeleteAll(activeAuthKeyringService()); err != nil {
		t.Fatalf("keyring.DeleteAll Wavo: %v", err)
	}
	if err := keyring.DeleteAll("chatwoot-cli"); err != nil {
		t.Fatalf("keyring.DeleteAll upstream: %v", err)
	}
	t.Setenv(config.APIKeyEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	blockConfigReplacement(t)
	if err := keyring.Set("chatwoot-cli", "api-key", "upstream-token"); err != nil {
		t.Fatalf("seed upstream credential: %v", err)
	}
	if err := keyring.Set(activeAuthKeyringService(), "unrelated-entry", "keep-me"); err != nil {
		t.Fatalf("seed unrelated Wavo credential: %v", err)
	}

	err := runLogin(t, server.URL+"\n", "new-wavo-token", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "failed to save config") {
		t.Fatalf("login() error = %v, want config save failure", err)
	}
	if strings.Contains(err.Error(), "new-wavo-token") {
		t.Fatalf("config error leaked token: %q", err)
	}
	cfg := &config.Config{BaseURL: server.URL, AccountID: 7}
	if _, _, err := config.ResolveAPIKey(cfg); !errors.Is(err, config.ErrAPIKeyNotFound) {
		t.Fatalf("new Wavo credential remained after rollback: %v", err)
	}
	if got, err := keyring.Get("chatwoot-cli", "api-key"); err != nil || got != "upstream-token" {
		t.Fatalf("rollback changed upstream credential: got=%q err=%v", got, err)
	}
	if got, err := keyring.Get(activeAuthKeyringService(), "unrelated-entry"); err != nil || got != "keep-me" {
		t.Fatalf("rollback changed unrelated Wavo credential: got=%q err=%v", got, err)
	}
}

func TestAuthLoginConfigFailureRestoresPreviousWavoLogin(t *testing.T) {
	server := loginProfileServer(t, `{"id":5,"name":"Eve","email":"eve@example.com","accounts":[{"id":7,"name":"Acme","role":"administrator"}]}`)
	defer server.Close()
	keyring.MockInit()
	if err := keyring.DeleteAll(activeAuthKeyringService()); err != nil {
		t.Fatalf("keyring.DeleteAll Wavo: %v", err)
	}
	t.Setenv(config.APIKeyEnv, "")

	oldHome := t.TempDir()
	t.Setenv("HOME", oldHome)
	oldConfig := &config.Config{BaseURL: server.URL, AccountID: 7, UserID: 4}
	if err := config.Save(oldConfig); err != nil {
		t.Fatalf("save old config: %v", err)
	}
	if err := config.SaveAPIKey(oldConfig, "old-wavo-token"); err != nil {
		t.Fatalf("save old credential: %v", err)
	}
	if err := keyring.Set(activeAuthKeyringService(), "unrelated-entry", "keep-me"); err != nil {
		t.Fatalf("seed unrelated Wavo credential: %v", err)
	}

	failedHome := t.TempDir()
	if err := os.Setenv("HOME", failedHome); err != nil {
		t.Fatalf("set failing HOME: %v", err)
	}
	blockConfigReplacement(t)
	err := runLogin(t, server.URL+"\n", "new-wavo-token", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "failed to save config") {
		t.Fatalf("login() error = %v, want config save failure", err)
	}

	if err := os.Setenv("HOME", oldHome); err != nil {
		t.Fatalf("restore old HOME: %v", err)
	}
	restoredConfig, err := config.Load()
	if err != nil || restoredConfig == nil || *restoredConfig != *oldConfig {
		t.Fatalf("old config changed: got=%#v err=%v want=%#v", restoredConfig, err, oldConfig)
	}
	if got, source, err := config.ResolveAPIKey(restoredConfig); err != nil || got != "old-wavo-token" || source != config.CredentialSourceKeyring {
		t.Fatalf("old login not usable: got=%q source=%q err=%v", got, source, err)
	}
	if got, err := keyring.Get(activeAuthKeyringService(), "unrelated-entry"); err != nil || got != "keep-me" {
		t.Fatalf("rollback changed unrelated Wavo credential: got=%q err=%v", got, err)
	}
}

func activeAuthKeyringService() string {
	if config.IsDev {
		return brand.DevKeyringService
	}
	return brand.KeyringService
}

func blockConfigReplacement(t *testing.T) {
	t.Helper()
	path, err := config.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("block config replacement: %v", err)
	}
}

// isolateAuthEnv gives a test its own HOME + mocked keyring and clears the
// WAVO_API_KEY override so credential resolution exercises the keyring path.
func isolateAuthEnv(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	if err := keyring.DeleteAll(brand.KeyringService); err != nil {
		t.Fatalf("keyring.DeleteAll: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
}

func loginProfileServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/profile" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func assertNoSavedLogin(t *testing.T, cfg *config.Config) {
	t.Helper()
	if got, err := config.Load(); err != nil || got != nil {
		t.Fatalf("config persisted unexpectedly: cfg=%#v err=%v", got, err)
	}
	if _, _, err := config.ResolveAPIKey(cfg); !errors.Is(err, config.ErrAPIKeyNotFound) {
		t.Fatalf("credential persisted unexpectedly: %v", err)
	}
}

func runLogin(t *testing.T, input, token string, writer io.Writer) error {
	t.Helper()
	return (&AuthLoginCmd{}).login(
		&App{},
		strings.NewReader(input),
		writer,
		true,
		func() (string, error) { return token, nil },
	)
}

func authFailureServer(t *testing.T, mode, token string) (*httptest.Server, string, string) {
	t.Helper()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect target received request with credential %q", r.Header.Get("api_access_token"))
		http.Error(w, "unexpected redirect", http.StatusInternalServerError)
	}))
	t.Cleanup(target.Close)
	location := target.URL + "/provider-diagnostic?raw=" + url.QueryEscape(token) +
		"&lower=" + percentEncodeLower(token) + "&mixed=" + percentEncodeMixed(token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reflected := r.Header.Get("api_access_token")
		if mode == "redirect" {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusFound)
			return
		}
		http.Error(w, "provider diagnostic "+strings.Join(credentialVariants(reflected), " | "), http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	return server, target.URL, location
}

func assertGenericAuthError(t *testing.T, err error, token string, urls ...string) {
	t.Helper()
	for _, disallowed := range append(credentialVariants(token), "provider diagnostic", "redirect refused") {
		if disallowed != "" && strings.Contains(err.Error(), disallowed) {
			t.Fatalf("auth error exposed %q: %q", disallowed, err)
		}
	}
	for _, rawURL := range urls {
		if rawURL != "" && strings.Contains(err.Error(), rawURL) {
			t.Fatalf("auth error exposed URL %q: %q", rawURL, err)
		}
	}
	if !strings.Contains(err.Error(), "wavo auth login") {
		t.Fatalf("auth error is not actionable: %q", err)
	}
}

func credentialVariants(token string) []string {
	candidates := []string{token, url.QueryEscape(token), url.PathEscape(token), percentEncodeLower(token), percentEncodeMixed(token)}
	unique := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			unique = append(unique, candidate)
		}
	}
	return unique
}

func percentEncodeLower(value string) string {
	const hex = "0123456789abcdef"
	var encoded strings.Builder
	for _, b := range []byte(value) {
		encoded.WriteByte('%')
		encoded.WriteByte(hex[b>>4])
		encoded.WriteByte(hex[b&0x0f])
	}
	return encoded.String()
}

func percentEncodeMixed(value string) string {
	const hex = "0123456789ABCDEF"
	var encoded strings.Builder
	for i, b := range []byte(value) {
		if i%2 == 0 && ((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')) {
			encoded.WriteByte(b)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hex[b>>4])
		encoded.WriteByte(hex[b&0x0f])
	}
	return encoded.String()
}

func reflectedProfile(t *testing.T, token string) *sdk.ProfileResponse {
	t.Helper()
	return &sdk.ProfileResponse{
		ID:                 5,
		Name:               "Name " + token + " Tail\x1b[31m",
		Email:              "mail-" + percentEncodeLower(token) + "-tail",
		Role:               "role " + percentEncodeMixed(token) + " safe",
		AvailabilityStatus: "available " + url.QueryEscape(token) + " safe",
		Accounts: []sdk.ProfileAccount{
			{ID: 7, Name: "Acme " + url.PathEscape(token) + " Safe", Role: "admin " + percentEncodeLower(token) + " Safe"},
			{ID: 9, Name: "Beta " + percentEncodeMixed(token) + " Safe", Role: "agent " + url.QueryEscape(token) + " Safe"},
		},
	}
}

func reflectedProfileJSON(t *testing.T, token string) string {
	t.Helper()
	body, err := json.Marshal(reflectedProfile(t, token))
	if err != nil {
		t.Fatalf("json.Marshal reflected profile: %v", err)
	}
	return string(body)
}

func assertNoCredentialReflection(t *testing.T, text, token string) {
	t.Helper()
	for _, variant := range credentialVariants(token) {
		if strings.Contains(text, variant) {
			t.Fatalf("display exposed credential variant %q: %q", variant, text)
		}
	}
	decoded := text
	for range 3 {
		queryDecoded, queryErr := url.QueryUnescape(decoded)
		pathDecoded, pathErr := url.PathUnescape(decoded)
		if (queryErr == nil && strings.Contains(queryDecoded, token)) ||
			(pathErr == nil && strings.Contains(pathDecoded, token)) {
			t.Fatalf("display reveals credential after URL decoding: %q", text)
		}
		if queryErr == nil && queryDecoded != decoded {
			decoded = queryDecoded
			continue
		}
		if pathErr == nil && pathDecoded != decoded {
			decoded = pathDecoded
			continue
		}
		break
	}
}
