package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"gopkg.in/yaml.v3"
)

type Config struct {
	BaseURL    string           `yaml:"base_url"`
	AccountID  int              `yaml:"account_id"`
	UserID     int              `yaml:"user_id,omitempty"`
	HelpCenter HelpCenterConfig `yaml:"help_center,omitempty"`
}

type HelpCenterConfig struct {
	DefaultPortalSlug string `yaml:"default_portal_slug,omitempty"`
	DefaultLocale     string `yaml:"default_locale,omitempty"`
}

func ConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, brand.ConfigDirName), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No config file exists
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	return saveWithReplace(cfg, replaceFile)
}

func saveWithReplace(cfg *Config, replace func(source, destination string) error) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("failed to secure config directory: %w", err)
	}

	path := filepath.Join(dir, configFileName)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	temp, err := os.CreateTemp(dir, "."+configFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary config: %w", err)
	}
	tempPath := temp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
		_ = os.Remove(tempPath)
	}()

	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("failed to secure temporary config: %w", err)
	}
	written, err := temp.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write temporary config: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("failed to write temporary config: %w", io.ErrShortWrite)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary config: %w", err)
	}
	closed = true
	if err := replace(tempPath, path); err != nil {
		return fmt.Errorf("failed to replace config: %w", err)
	}

	// Replacement is the commit point. Directory syncing improves durability on
	// filesystems that support it, but failure must not report the committed save
	// as failed and trigger credential rollback.
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func (c *Config) IsValid() bool {
	return strings.TrimSpace(c.BaseURL) != "" && c.AccountID > 0
}
