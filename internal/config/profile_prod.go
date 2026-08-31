//go:build !dev

package config

import "github.com/iRaxe/wavo-cli/internal/brand"

// Production build profile. This is the default for `go build` and every
// released binary (goreleaser passes no build tags), so credentials live in the
// canonical config file and keyring service.
//
// The dev counterpart lives in profile_dev.go behind `//go:build dev` and is
// never compiled into release builds — building for prod disables that path.
const (
	configFileName = "config.yaml"
	keyringService = brand.KeyringService

	// IsDev reports whether this binary was built with `-tags dev`.
	IsDev = false
)
