//go:build dev

package config

import "github.com/iRaxe/wavo-cli/internal/brand"

// Dev build profile, compiled in only with `go build -tags dev` (which is what
// `mise run dev` uses). Dev builds read a separate config file and store their
// token under a separate keyring service, so iterating on the CLI never reads,
// overwrites, or — on logout — deletes the production credentials managed by an
// installed binary.
//
// None of this is compiled into release builds; see profile_prod.go.
const (
	configFileName = "config.dev.yaml"
	keyringService = brand.DevKeyringService

	// IsDev reports whether this binary was built with `-tags dev`.
	IsDev = true
)
