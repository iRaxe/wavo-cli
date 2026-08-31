#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd -P)
checker="$repo_root/scripts/check-workflow.sh"
test_root=$(mktemp -d /tmp/wavo-workflow-test.XXXXXX)
cleanup() {
  cleanup_status=$?
  trap - EXIT HUP INT TERM
  rm -rf "$test_root"
  exit "$cleanup_status"
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM

fail() { printf 'not ok - %s\n' "$1" >&2; exit 1; }
pass() { printf 'ok - %s\n' "$1"; }

test -x "$checker" || fail 'workflow checker exists and is executable'
mkdir -p "$test_root/.github/workflows" "$test_root/scripts"
cp "$checker" "$test_root/scripts/check-workflow.sh"
chmod 0755 "$test_root/scripts/check-workflow.sh"
fixture_checker="$test_root/scripts/check-workflow.sh"

cat >"$test_root/.github/workflows/ci.yml" <<'EOF'
jobs:
  safe:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
      - run: go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
      - uses: golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a
        with:
          version: v2.13.2
EOF
"$fixture_checker" >/dev/null || fail 'pinned workflow was rejected'
pass 'pinned workflow tools and actions pass'

sed 's/@v1.7.0/@latest/' "$test_root/.github/workflows/ci.yml" >"$test_root/.github/workflows/unsafe.yml"
if "$fixture_checker" >/dev/null 2>&1; then fail 'go install at latest was accepted'; fi
pass 'floating go install tools are rejected'

sed 's/version: v2.13.2/version: latest/' "$test_root/.github/workflows/ci.yml" >"$test_root/.github/workflows/unsafe.yml"
if "$fixture_checker" >/dev/null 2>&1; then fail 'latest executable version was accepted'; fi
pass 'floating executable action inputs are rejected without matching runner labels'

sed 's#checkout@3d3c42e5aac5ba805825da76410c181273ba90b1#checkout@v7#' "$test_root/.github/workflows/ci.yml" >"$test_root/.github/workflows/unsafe.yml"
if "$fixture_checker" >/dev/null 2>&1; then fail 'floating action ref was accepted'; fi
pass 'floating action refs are rejected'

sed 's#- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1#- name: Checkout\
        uses: actions/checkout@v7#' "$test_root/.github/workflows/ci.yml" >"$test_root/.github/workflows/unsafe.yml"
if "$fixture_checker" >/dev/null 2>&1; then fail 'named-step floating action ref was accepted'; fi
pass 'floating action refs beneath named steps are rejected'

sed 's|checkout@3d3c42e5aac5ba805825da76410c181273ba90b1|checkout@v7  # mutable ref|' "$test_root/.github/workflows/ci.yml" >"$test_root/.github/workflows/unsafe.yml"
if "$fixture_checker" >/dev/null 2>&1; then fail 'commented floating action ref was accepted'; fi
pass 'floating action refs with trailing comments are rejected'
