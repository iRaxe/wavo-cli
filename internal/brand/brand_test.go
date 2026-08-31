package brand

import "testing"

func TestProductionIdentity(t *testing.T) {
	t.Parallel()
	wants := map[string]string{
		"command": Command, "product": Product,
		"base": DefaultBaseURL, "installer": InstallURL,
		"repo": Repository, "release": LatestReleaseURL,
		"env": APIKeyEnv, "dir": ConfigDirName,
		"keyring": KeyringService, "dev-keyring": DevKeyringService,
	}
	expected := map[string]string{
		"command": "wavo", "product": "Wavo",
		"base":      "https://my.wavo.it",
		"installer": "https://my.wavo.it/install-cli",
		"repo":      "iRaxe/wavo-cli",
		"release":   "https://api.github.com/repos/iRaxe/wavo-cli/releases/latest",
		"env":       "WAVO_API_KEY", "dir": ".wavo",
		"keyring": "wavo-cli", "dev-keyring": "wavo-cli-dev",
	}
	for key, want := range expected {
		if got := wants[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
