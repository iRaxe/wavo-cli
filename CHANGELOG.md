# Changelog

All notable changes to wavo-cli are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Per-conversation lock for all mutating `conv` verbs (`reply`, `resolve`, `open`, `pending`, `snooze`, `assign`, `unassign`, `label`, `priority`): concurrent mutations of the same conversation from separate terminals now fail fast instead of both running. Locks live in `~/.wavo/locks/` and are released automatically by the OS if the process dies.

### Changed

### Fixed

## [0.6.1] - 2026-06-03

### Fixed

- Release workflow wrote the extracted release notes into the working tree, leaving git dirty and causing GoReleaser to abort with a "dirty state" error. The notes are now written to the runner temp directory, and `release_notes.md` is gitignored as a safety net for local dry runs.

## [0.6.0] - 2026-06-03

### Added

- Help center commands for managing portals and articles.

### Changed

- Reworked the authentication flow.
- Updated bundled usage examples.
- Bumped `github.com/getkin/kin-openapi` from 0.138.0 to 0.139.0.
- Bumped `actions/upload-artifact` from 4 to 7.

### Fixed

- Hardened the install script and credential cleanup.

## [0.5.0] - 2026-05-13

Adds a raw API escape hatch for cases where the CLI does not yet have a first-class command. There are no breaking changes.

### Added

- **`wavo api` raw API command** — make authenticated Wavo API requests directly from the CLI. Account-relative paths such as `/conversations/123` are expanded automatically under `/api/v1/accounts/<account_id>`; use `--exact` to target absolute paths like `/api/v1/profile`. Supports custom methods (`-X`), JSON request bodies, file/stdin bodies (`--data`), and additional headers. JSON responses are pretty-printed; non-JSON responses are passed through as-is.

  ```bash
  wavo api /conversations/123
  wavo api -X PATCH /conversations/123 --data '{"status":"open"}'
  wavo api --exact /api/v1/profile
  ```

### Changed

- Updated the bundled agent skill with stronger raw API guidance: prefer first-class commands, consult the Wavo Swagger before raw calls, and treat all writes (including non-GET raw API calls) as privileged actions requiring explicit user confirmation.
- Simplified the README to focus on core setup and usage, delegating full command details to the docs site.

## [0.4.0] - 2026-05-11

### Added

- **`wavo conv contact` command** — fetch the contact attached to a conversation directly from the conversation id, without a separate contacts lookup.
- **Version check** — the CLI now checks for newer releases so users know when an update is available.
- **Smarter install script** — detects an existing install, skips the completion prompt on rerun, and uses the `install` command when available.

### Changed

- Improved CI and tooling: added `govulncheck`, a `go mod tidy` diff check, a coverage task with a sticky PR coverage comment (gated to same-repo PRs), and dependency review for PRs. Bumped GitHub Actions versions and updated the Go toolchain.
- Removed the bundled `.claude` directory.
- Bumped `github.com/alecthomas/kong` from 1.14.0 to 1.15.0.
- Bumped `github.com/getkin/kin-openapi` from 0.133.0 to 0.138.0.
- Bumped `golang.org/x/term` from 0.31.0 to 0.43.0.

## [0.3.0] - 2026-05-11

### Added

- **Agent skills** — bundled skill definitions for driving the CLI from agent workflows.
- Pinned GoReleaser for reproducible release builds.

### Fixed

- Avoid caching the user id when authenticating from an environment token.
- Prevent account override from persisting across invocations.
- Redact tokens in verbose logs.
- Sanitize untrusted output fields.
- Validate `smoke.sh` base URL against URL userinfo bypass.
- Pin the zsh completion source to the install path.
- Null-delimit the find loop in the mise agents task.
- Drop the project `bin/` from the mise `PATH`.
- Fixed the docs link, spellcheck errors, the Go version, and skill label usage.

## [0.2.0] - 2026-05-08

### Added

- **`version` command** to report the installed CLI version.
- **`whoami` command**, unified with `me` / `auth status`.
- Better shell completions.
- Better install script.
- CI for tests.

### Fixed

- Clean exit when changing directory.
- Handle output writer errors to satisfy `errcheck`.

## [0.1.0] - 2026-05-08

First release. A CLI for Wavo that reads and writes the same Wavo API your dashboard uses.

### Added

- List conversations, view details, reply, change status, assign, label, and set priority.
- Noun-grammar CLI with id-first verb dispatch and a custom Kong help printer.
- SDK services for conversations, contacts, inboxes, agents, and profile, including conversation writes and subresources.
- Mentions support in private notes, including team mentions.
- Message rendering with pagination and autofetch.
- API keys stored in the OS keyring; hardened auth input and storage.
- Shell completion support.
- Release pipeline and smoke test.
- Pre-built binaries for macOS, Linux (x86_64 + arm64), and Windows.

Install with `curl -fsSL https://my.wavo.it/install-cli | sh`, then `wavo auth login`.

[Unreleased]: https://github.com/iRaxe/wavo-cli/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/iRaxe/wavo-cli/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/iRaxe/wavo-cli/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/iRaxe/wavo-cli/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/iRaxe/wavo-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/iRaxe/wavo-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/iRaxe/wavo-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/iRaxe/wavo-cli/releases/tag/v0.1.0
