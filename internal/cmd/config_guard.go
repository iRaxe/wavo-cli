package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/iRaxe/wavo-cli/internal/config"
	"github.com/iRaxe/wavo-cli/internal/lock"
)

// updateConfigForAuth serializes a config mutation with login/logout and
// refuses to apply data obtained under a different Wavo authentication scope.
func updateConfigForAuth(snapshot *config.Config, mutate func(*config.Config) error) (*config.Config, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("authentication for Wavo is unavailable; reload and retry the command")
	}

	authLock, err := lock.AcquireAuth()
	if err != nil {
		return nil, fmt.Errorf("cannot safely update Wavo authentication; retry the command: %w", err)
	}
	defer authLock.Release()

	current, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("cannot reload Wavo authentication; retry the command: %w", err)
	}
	if current == nil || !sameAuthTuple(current, snapshot) {
		return nil, fmt.Errorf("authentication for Wavo changed; reload and retry the command")
	}
	if err := mutate(current); err != nil {
		return nil, err
	}
	if err := config.Save(current); err != nil {
		return nil, fmt.Errorf("failed to save Wavo configuration: %w", err)
	}
	return current, nil
}

func sameAuthTuple(left, right *config.Config) bool {
	return left != nil && right != nil &&
		left.AccountID == right.AccountID &&
		left.UserID == right.UserID &&
		normalizedAuthBaseURL(left.BaseURL) == normalizedAuthBaseURL(right.BaseURL)
}

func normalizedAuthBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/")
}
