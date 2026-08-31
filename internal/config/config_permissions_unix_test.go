//go:build !windows

package config

import (
	"os"
	"testing"
)

func assertSecureConfigFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %o, want 600", info.Mode().Perm())
	}
}

func assertSecureConfigDirectory(t *testing.T) {
	t.Helper()
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", dir, err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("config directory mode = %o, want 700", info.Mode().Perm())
	}
}
