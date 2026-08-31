//go:build dev

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/brand"
)

// Guards the dev build profile. Run with `go test -tags dev ./internal/config/`.
// Dev builds must read a separate config file and keyring entry so development
// never touches production credentials.
func TestDevBuildProfile(t *testing.T) {
	if !IsDev {
		t.Fatal("IsDev = false in a dev build")
	}
	if configFileName != "config.dev.yaml" {
		t.Fatalf("configFileName = %q, want config.dev.yaml", configFileName)
	}
	if keyringService != brand.DevKeyringService {
		t.Fatalf("keyringService = %q, want %q", keyringService, brand.DevKeyringService)
	}

	t.Setenv("HOME", t.TempDir())
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error = %v", err)
	}
	if want := filepath.Join(os.Getenv("HOME"), brand.ConfigDirName, "config.dev.yaml"); path != want {
		t.Fatalf("ConfigPath() = %q, want %q", path, want)
	}
}
