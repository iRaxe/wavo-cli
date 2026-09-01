# wavo-cli

Source repository for the Wavo CLI beta and its bundled AI-agent skill. Keep
customer documentation aligned with the currently announced pinned beta tag.

## Build & Run

```bash
mise run build                            # writes ./bin/wavo
mise run dev                              # auto-rebuild with dev profile
./bin/wavo                                # no args -> prints help
./bin/wavo convs                          # plural noun = list
./bin/wavo conv 123                       # singular + ID = view
./bin/wavo conv 123 reply "hi"            # ID-first verb dispatch
```

## Project Structure

```text
cmd/wavo/main.go           Entry point: pre-parses ID-first grammar, then Kong
internal/
  sdk/                 HTTP client and service modules
  cmd/                 Kong command structs with Run(app *App) error
  config/              Nonsecret YAML config and OS-keyring credentials
  output/              Text, JSON, CSV, and ID-only output
skills/wavo-cli/       Bundled Wavo AI-agent skill
docs/BETA.md           Beta operator and customer onboarding
```

## Architecture

- **CLI framework**: Kong (alecthomas/kong), using a struct-based command tree
- **SDK pattern**: `client.Conversations()`, `client.Contacts()`, and other
  methods return service objects
- **Command pattern**: each command is a struct with `Run(app *App) error`
- **App struct** holds `Client`, `Printer`, `Config`, and `Version` — passed to
  commands

## Key Conventions

- Kong commands define flags and arguments as tagged struct fields and implement
  `Run(app *App) error`.
- Plural nouns list. Singular nouns own verb subcommands, whose structs hold the
  `arg:""` ID. Kong cannot mix `arg:""` and `cmd:""` siblings, so it parses the
  verb before the ID internally (`conv reply 123 "hi"`).
- `cmd/wavo/main.go` runs `rewriteIDFirstGrammar` to swap
  `<noun> <id> <verb>` to `<noun> <verb> <id>` before Kong parses. Its custom
  `kong.Help` printer restores ID-first help.
- `default:"withargs"` routes `wavo conv 123` through the `View` subcommand.
- `skipAuth` in main.go: `auth`, `config`, `completion`, `me`, `whoami`, and
  `version` bypass ordinary authenticated client creation
- `GetRaw()` handles non-account-scoped endpoints such as `/api/v1/profile`.
- `Config.UserID` is cached on `auth login` and refreshed by `auth status` when
  a keyring-backed profile changes.
- Production builds use `~/.wavo/config.yaml` and keyring service `wavo-cli`.
  `-tags dev` builds use `config.dev.yaml` and `wavo-cli-dev` so development
  cannot read or remove production credentials.
- `auth login --headless` validates `WAVO_API_KEY` and persists only nonsecret
  config. It must never read or write the operating-system keyring or serialize
  the token.

## Wavo API Quirks

- Contacts list `meta.current_page` returns as string, not int
- Messages list `meta.agent_last_seen_at` can be string
- Single contact GET returns `{payload: {contact data}}` (wrapped)
- Agents list returns raw `[]Agent` array (not wrapped in payload)
- Profile endpoint is non-account-scoped: `/api/v1/profile`

## Commits

Use conventional commits without scope: `feat:`, `fix:`, `chore:`, `refactor:`,
or `docs:`.

## Documentation and release status

- `README.md` and `docs/BETA.md` describe an announced-beta flow. Before the
  announcement, keep an explicit pre-release caveat near installer and package
  commands.
- Customer-visible and provider-visible strings must say Wavo. Run
  `scripts/check-branding.sh`, `scripts/check-branding-test.sh`,
  `scripts/check-docs.sh`, and `scripts/check-docs-test.sh` after doc or skill
  changes.
- Keep the skill grammar aligned with generated `wavo --help`; mutations must
  retain the per-command, per-target, per-workspace approval boundary.

## Releasing

Releases are tag-driven and the GitHub release body comes from `CHANGELOG.md`.

1. Add the exact tag version as `## [<version>] - YYYY-MM-DD` in `CHANGELOG.md`.
   `<version>` may be stable or prerelease SemVer.
2. Commit, then push tag `v<version>` (the changelog version with a leading
   `v`). Do not push a tag or publish a release without explicit authorization.
3. The `release` workflow (`.github/workflows/release.yml`) runs tests, extracts
   that version's section, and passes it to GoReleaser. A missing matching
   section fails the release.
4. GoReleaser (`.goreleaser.yml`) builds cross-platform archives, checksums, and
   the GitHub release. Its `changelog:` block is a fallback if `--release-notes`
   is ever removed.

Dry run (local goreleaser must be v2; CI pins `2.15.4`):

```bash
go run github.com/goreleaser/goreleaser/v2@v2.15.4 check
go run github.com/goreleaser/goreleaser/v2@v2.15.4 release --snapshot --clean --skip=publish
```
