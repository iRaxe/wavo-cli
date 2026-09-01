# Changelog

## [Unreleased]

### Added

- Native contact filtering and pagination for conversation lists through
  `wavo convs --contact <id>` and `wavo contact <id> conversations`, backed by
  the Wavo conversation filter API.
- `--status all`, `--assignee assigned`, `--inbox`, `--team`, `--label`, and
  `--page` filters on `wavo contact <id> conversations`.

### Changed

- `wavo contact <id> conversations` now reads the conversation filter endpoint
  instead of the unfiltered contact conversations endpoint, so `-o json` returns
  the standard conversation-list `.data.payload[]` shape.

## [0.1.0-beta.2] - 2026-09-01

### Fixed

- Added a secure headless login flow that validates `WAVO_API_KEY` and saves
  only nonsecret workspace configuration.
- Detects an unavailable operating-system keyring before asking for a token and
  points server operators to `wavo auth login --headless`.

## [0.1.0-beta.1] - 2026-08-31

### Added

- Wavo-branded CLI for conversations, contacts, assignments, and help centers.
- Guided Wavo login with operating-system keyring storage.
- Wavo AI-agent skill with prompt-injection and write-approval safeguards.
- Linux, macOS, and Windows release archives with SHA-256 checksums.
