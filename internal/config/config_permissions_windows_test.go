//go:build windows

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
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("config file is unexpectedly read-only: mode %o", info.Mode().Perm())
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
	if !info.IsDir() {
		t.Fatalf("config path %q is not a directory", dir)
	}
}

func TestWindowsConfigWritableAndReadOnlySemantics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	first := &Config{BaseURL: "https://first.wavo.example", AccountID: 7}
	if err := Save(first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error = %v", err)
	}
	assertSecureConfigFile(t, path)

	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("Chmod(read-only) error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(read-only) error = %v", err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("read-only config reports writable mode %o", info.Mode().Perm())
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod(writable) error = %v", err)
	}

	second := &Config{BaseURL: "https://second.wavo.example", AccountID: 9, UserID: 22}
	if err := Save(second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}
	loaded, err := Load()
	if err != nil || loaded == nil || *loaded != *second {
		t.Fatalf("Load() = %#v, %v; want %#v", loaded, err, second)
	}
	assertSecureConfigFile(t, path)
}
