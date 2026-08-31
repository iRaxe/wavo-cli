package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/iRaxe/wavo-cli/internal/config"
	"github.com/iRaxe/wavo-cli/internal/lock"
	"github.com/iRaxe/wavo-cli/internal/output"
	"github.com/iRaxe/wavo-cli/internal/sdk"
	"golang.org/x/term"
)

type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"" help:"Login with your Wavo credentials."`
	Logout AuthLogoutCmd `cmd:"" help:"Logout and remove saved credentials."`
	Status AuthStatusCmd `cmd:"" help:"Show current authentication status."`
}

type AuthLoginCmd struct{}

var (
	errLoginProfile  = errors.New("Wavo authentication failed. Verify credentials or run 'wavo auth login'.")
	errStatusProfile = errors.New("Wavo profile unavailable. Check availability or run 'wavo auth login'.")
)

func (c *AuthLoginCmd) Run(app *App) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("interactive login requires a terminal; set %s for headless use", config.APIKeyEnv)
	}
	reader := bufio.NewReader(os.Stdin)
	return c.login(app, reader, os.Stdout, config.IsDev, func() (string, error) { return readAPIKey(reader) })
}

func (c *AuthLoginCmd) login(_ *App, reader io.Reader, writer io.Writer, allowHTTP bool, readToken func() (string, error)) error {
	bufferedReader := bufio.NewReader(reader)

	fmt.Fprintln(writer, "Wavo CLI Login")
	fmt.Fprintln(writer, "==============")

	fmt.Fprintf(writer, "Base URL (default: %s): ", brand.DefaultBaseURL)
	baseURLInput, err := bufferedReader.ReadString('\n')
	if err != nil && baseURLInput == "" {
		return fmt.Errorf("failed to read base URL: %w", err)
	}
	baseURL, err := normalizeLoginBaseURL(baseURLInput, allowHTTP)
	if err != nil {
		return err
	}

	apiKey, err := readToken()
	if err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}

	client := sdk.NewClient(baseURL, apiKey, 0)
	profile, err := client.Profile().Get()
	if err != nil {
		return errLoginProfile
	}
	displayProfile := safeProfileForDisplay(profile, apiKey)

	accountID, err := selectAccount(bufferedReader, displayProfile.Accounts, profile.AccountID, writer)
	if err != nil {
		return err
	}
	if err := verifyAccountAccess(profile, accountID); err != nil {
		return err
	}
	cfg := &config.Config{BaseURL: baseURL, AccountID: accountID, UserID: profile.ID}

	if err := persistLogin(cfg, apiKey); err != nil {
		if errors.Is(err, lock.ErrLocked) {
			return err
		}
		return fmt.Errorf("%s", redactAuthError(err.Error(), apiKey))
	}

	fmt.Fprint(writer, loginSuccessMessage(displayProfile.Name, displayProfile.Email))
	fmt.Fprintf(writer, "Workspace: %s\n", selectedAccount(displayProfile.Accounts, accountID))
	return nil
}

func persistLogin(cfg *config.Config, apiKey string) error {
	authLock, err := lock.AcquireAuth()
	if err != nil {
		return fmt.Errorf("failed to persist Wavo login: %w", err)
	}
	defer authLock.Release()

	rollbackCredential, err := config.SaveAPIKeyWithRollback(cfg, apiKey)
	if err != nil {
		return err
	}

	if err := config.Save(cfg); err != nil {
		if rollbackErr := rollbackCredential(); rollbackErr != nil {
			return fmt.Errorf("failed to save config: %w (also failed to restore the previous Wavo credential: %v)", err, rollbackErr)
		}
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func normalizeLoginBaseURL(input string, allowHTTP bool) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		raw = brand.DefaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return "", fmt.Errorf("base URL must contain only scheme and host")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return "", fmt.Errorf("base URL must use HTTPS")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

type profileDisplayAccount struct {
	ID   int
	Name string
	Role string
}

type profileDisplay struct {
	Name               string
	Email              string
	Role               string
	AvailabilityStatus string
	Accounts           []profileDisplayAccount
}

func selectAccount(reader io.Reader, accounts []profileDisplayAccount, fallbackID int, writer io.Writer) (int, error) {
	switch len(accounts) {
	case 0:
		if fallbackID > 0 {
			return fallbackID, nil
		}
		return 0, fmt.Errorf("authenticated Wavo profile has no accessible workspace")
	case 1:
		return accounts[0].ID, nil
	}
	for i, account := range accounts {
		fmt.Fprintf(writer, "%d. %s (%d, %s)\n", i+1, output.SanitizeText(account.Name), account.ID, output.SanitizeText(account.Role))
	}
	fmt.Fprint(writer, "Workspace: ")
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && line == "" {
		return 0, fmt.Errorf("failed to read workspace selection: %w", err)
	}
	selection, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || selection < 1 || selection > len(accounts) {
		return 0, fmt.Errorf("workspace selection must be a number from 1 to %d", len(accounts))
	}
	return accounts[selection-1].ID, nil
}

func selectedAccount(accounts []profileDisplayAccount, id int) string {
	for _, account := range accounts {
		if account.ID == id {
			name := output.SanitizeText(account.Name)
			if name == "" {
				return strconv.Itoa(id)
			}
			return fmt.Sprintf("%d (%s)", id, name)
		}
	}
	return strconv.Itoa(id)
}

func safeProfileForDisplay(profile *sdk.ProfileResponse, token string) *profileDisplay {
	if profile == nil {
		return nil
	}
	safe := &profileDisplay{
		Name:               safeProfileText(profile.Name, token),
		Email:              safeProfileText(profile.Email, token),
		Role:               safeProfileText(profile.Role, token),
		AvailabilityStatus: safeProfileText(profile.AvailabilityStatus, token),
		Accounts:           make([]profileDisplayAccount, len(profile.Accounts)),
	}
	for i, account := range profile.Accounts {
		safe.Accounts[i] = profileDisplayAccount{
			ID:   account.ID,
			Name: safeProfileText(account.Name, token),
			Role: safeProfileText(account.Role, token),
		}
	}
	return safe
}

func safeProfileText(value, token string) string {
	if token != "" {
		value = strings.ReplaceAll(value, token, "[REDACTED]")
		if strings.ContainsAny(value, "%+") {
			return "[REDACTED]"
		}
	}
	return output.SanitizeText(value)
}

func redactAuthError(message, token string) string {
	return safeProfileText(message, token)
}

func loginSuccessMessage(name, email string) string {
	return fmt.Sprintf("Logged in as %s (%s)\n", output.SanitizeText(name), output.SanitizeText(email))
}

// verifyAccountAccess rejects a selected workspace unless the authenticated
// profile lists it, or its fallback account_id matches for older instances.
func verifyAccountAccess(profile *sdk.ProfileResponse, accountID int) error {
	if accountID <= 0 {
		return fmt.Errorf("authenticated Wavo profile returned an invalid workspace ID")
	}
	if len(profile.Accounts) == 0 {
		if profile.AccountID == accountID {
			return nil
		}
		return fmt.Errorf("workspace %d is not available in the authenticated Wavo profile", accountID)
	}
	for _, acc := range profile.Accounts {
		if acc.ID == accountID {
			return nil
		}
	}
	return fmt.Errorf("workspace %d is not accessible with this API key; %s", accountID, accessibleAccountsHint(profile.Accounts))
}

func accessibleAccountsHint(accounts []sdk.ProfileAccount) string {
	parts := make([]string, 0, len(accounts))
	for _, acc := range accounts {
		if name := output.SanitizeText(acc.Name); name != "" {
			parts = append(parts, fmt.Sprintf("%d (%s)", acc.ID, name))
		} else {
			parts = append(parts, strconv.Itoa(acc.ID))
		}
	}
	if len(parts) == 1 {
		return "this key can access account " + parts[0]
	}
	return "this key can access accounts: " + strings.Join(parts, ", ")
}

func readAPIKey(reader *bufio.Reader) (string, error) {
	fmt.Print("API Key: ")

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		apiKey, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("failed to read API key: %w", err)
		}
		return strings.TrimSpace(string(apiKey)), nil
	}

	apiKey, err := reader.ReadString('\n')
	if err != nil && apiKey == "" {
		return "", fmt.Errorf("failed to read API key: %w", err)
	}
	return strings.TrimSpace(apiKey), nil
}

type AuthLogoutCmd struct{}

func (c *AuthLogoutCmd) Run(app *App) error {
	authLock, err := lock.AcquireAuth()
	if err != nil {
		return fmt.Errorf("failed to update Wavo authentication: %w", err)
	}
	defer authLock.Release()

	path, err := config.ConfigPath()
	if err != nil {
		return err
	}

	if err := config.DeleteAPIKey(nil); err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Not logged in.")
			if strings.TrimSpace(os.Getenv(config.APIKeyEnv)) != "" {
				fmt.Printf("%s is set in your environment; logout cannot remove environment-provided credentials.\n", config.APIKeyEnv)
			}
			return nil
		}
		return fmt.Errorf("Wavo credentials were removed, but the config file could not be removed: %w", err)
	}

	fmt.Println("Logged out successfully.")
	if strings.TrimSpace(os.Getenv(config.APIKeyEnv)) != "" {
		fmt.Printf("%s is set in your environment; logout cannot remove environment-provided credentials.\n", config.APIKeyEnv)
	}
	return nil
}

type AuthStatusCmd struct{}

func (c *AuthStatusCmd) Run(app *App) error { return runAuthStatus(app) }

// runAuthStatus is the shared implementation behind `auth status`, `me`, and
// `whoami`. They all answer "who am I and where am I logged in?" so they
// share output. It also opportunistically refreshes the cached UserID.
func runAuthStatus(app *App) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg == nil || !cfg.IsValid() {
		_, err := fmt.Fprintln(app.Printer.Writer, "Not logged in. Run 'wavo auth login' to authenticate.")
		return err
	}

	apiKey, source, err := config.ResolveAPIKey(cfg)
	if err != nil {
		return fmt.Errorf("not authenticated: %w", err)
	}

	client := sdk.NewClient(cfg.BaseURL, apiKey, cfg.AccountID)
	profile, err := client.Profile().Get()
	if err != nil {
		return errStatusProfile
	}
	displayProfile := safeProfileForDisplay(profile, apiKey)

	// Self-heal the cached UserID for older saved logins. Environment tokens are
	// temporary overrides and must not rewrite the persisted login identity.
	if source == config.CredentialSourceKeyring && cfg.UserID != profile.ID {
		if err := refreshCachedUserID(cfg, profile.ID); err != nil {
			return fmt.Errorf("failed to update Wavo authentication status: %w", err)
		}
	}

	app.Printer.PrintDetail([]output.KeyValue{
		{Key: "Instance", Value: cfg.BaseURL},
		{Key: "Account", Value: strconv.Itoa(cfg.AccountID)},
		{Key: "Workspace", Value: selectedAccount(displayProfile.Accounts, cfg.AccountID)},
		{Key: "User ID", Value: strconv.Itoa(profile.ID)},
		{Key: "Name", Value: displayProfile.Name},
		{Key: "Email", Value: displayProfile.Email},
		{Key: "Role", Value: displayProfile.Role},
		{Key: "Availability", Value: displayProfile.AvailabilityStatus},
		{Key: "Credential", Value: string(source)},
	})

	return nil
}

func refreshCachedUserID(snapshot *config.Config, profileID int) error {
	_, err := updateConfigForAuth(snapshot, func(current *config.Config) error {
		current.UserID = profileID
		return nil
	})
	return err
}
