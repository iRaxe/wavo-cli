# internal/config - Configuration Management

YAML persistence holds nonsecret account settings. Production configuration is
stored in `~/.wavo/config.yaml`. API keys resolve from `WAVO_API_KEY` first, then
the operating-system keyring.

## Files

### config.go

Configuration struct and file I/O. Provides:

- `Config` with base URL, account/user IDs, and help-center defaults
- `Load()` — read the active profile config; return `nil` when it is absent
- `Save()` — write non-secret YAML
- Validation: ensures BaseURL and AccountID are set before API calls
- Error handling: distinguishes between missing file and parse errors

### credentials.go

Credential resolution and OS keyring storage. Provides:

- `ResolveAPIKey()` — `WAVO_API_KEY` first, then OS keyring
- `SaveAPIKey()` — write validated login token to keyring
- `DeleteAPIKey()` — remove saved keyring token on logout

## Build Profiles (dev vs prod)

`configFileName` and `keyringService` are selected at build time via the `dev`
build tag (`profile_prod.go` for `//go:build !dev`, `profile_dev.go` for
`//go:build dev`):

| Profile | Config file | Keyring service | `config.IsDev` |
| --- | --- | --- | --- |
| Production | `~/.wavo/config.yaml` | `wavo-cli` | `false` |
| Dev (`-tags dev`) | `~/.wavo/config.dev.yaml` | `wavo-cli-dev` | `true` |

A dev build keeps its own credentials, so iterating on the CLI never reads or
clobbers the production login. The keyring **service** (not just the entry name)
is namespaced per profile, so `auth logout` — which does
`keyring.DeleteAll(keyringService)` to clear stale entries — only wipes the
active build's tokens and never the other profile's. Release builds (goreleaser
passes no tags) exclude `profile_dev.go` entirely — the dev path is compiled
out. `config view` shows a `Profile: dev` line on dev builds.

## Config Schema

```yaml
base_url: https://my.wavo.it
account_id: 47
user_id: 123
help_center:
  default_portal_slug: support
  default_locale: en
```

## Usage

In `main.go`:

```go
cfg, err := config.Load()
if err != nil {
    // Handle missing/invalid config
}

apiKey, _, err := config.ResolveAPIKey(cfg)
if err != nil {
    // Handle missing credentials
}

client := sdk.NewClient(cfg.BaseURL, apiKey, cfg.AccountID)
```

## Validation Rules

- **BaseURL** (required): full URL like `https://my.wavo.it`
- **AccountID** (required): numeric account ID from Wavo

## File Permissions

The config directory is created with `0700`; config files use `0600`. API keys
are not written to YAML.

The production and development build profiles are fixed compile-time profiles,
not user-selectable multi-account profiles.
