//go:build !dev

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/brand"
)

// Guards the production build profile. Runs under the default (untagged) build;
// the dev counterpart is profile_dev_test.go behind //go:build dev.
func TestProdBuildProfile(t *testing.T) {
	if IsDev {
		t.Fatal("IsDev = true in a non-dev build")
	}
	if configFileName != "config.yaml" {
		t.Fatalf("configFileName = %q, want config.yaml", configFileName)
	}
	if keyringService != brand.KeyringService {
		t.Fatalf("keyringService = %q, want %q", keyringService, brand.KeyringService)
	}

	t.Setenv("HOME", t.TempDir())
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error = %v", err)
	}
	if want := filepath.Join(os.Getenv("HOME"), brand.ConfigDirName, "config.yaml"); path != want {
		t.Fatalf("ConfigPath() = %q, want %q", path, want)
	}
}
