#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd -P)
repo_root=$(CDPATH='' cd "$script_dir/.." && pwd -P)
checker="$repo_root/scripts/check-docs.sh"

fail() {
  printf 'not ok - %s\n' "$1" >&2
  exit 1
}

pass() {
  printf 'ok - %s\n' "$1"
}

if [ ! -x "$checker" ]; then
  fail "documentation checker exists and is executable"
fi

temporary_dir=$(mktemp -d /tmp/wavo-doc-check-test.XXXXXX)
cleanup() {
  case "$temporary_dir" in
    /tmp/wavo-doc-check-test.*) rm -rf "$temporary_dir" ;;
    *) fail "refusing to remove unexpected temporary directory" ;;
  esac
}
trap cleanup EXIT HUP INT TERM

fixture="$temporary_dir/fixture"
mkdir -p "$fixture/scripts" "$fixture/docs" \
  "$fixture/skills/wavo-cli/references"
cp "$checker" "$fixture/scripts/check-docs.sh"
chmod +x "$fixture/scripts/check-docs.sh"

pinned_install='curl -fsSL https://my.wavo.it/install-cli | env WAVO_VERSION=v0.1.0-beta.2 sh'
headless_login='wavo auth login --headless'
printf '%s\n%s\n' "$pinned_install" "$headless_login" >"$fixture/README.md"
printf '%s\n%s\n' "$pinned_install" "$headless_login" >"$fixture/docs/BETA.md"
cp "$repo_root/skills/wavo-cli/SKILL.md" \
  "$fixture/skills/wavo-cli/SKILL.md"
cp "$repo_root/skills/wavo-cli/references/raw-api.md" \
  "$fixture/skills/wavo-cli/references/raw-api.md"

if "$fixture/scripts/check-docs.sh" >/dev/null; then
  pass "valid prerelease docs and packaged reference pass"
else
  fail "valid prerelease docs and packaged reference pass"
fi

printf '%s\n' 'curl -fsSL https://my.wavo.it/install-cli | sh' \
  >"$fixture/README.md"
if "$fixture/scripts/check-docs.sh" >/dev/null 2>&1; then
  fail "unpinned prerelease installer is rejected"
else
  pass "unpinned prerelease installer is rejected"
fi
printf '%s\n%s\n' "$pinned_install" "$headless_login" >"$fixture/README.md"

printf '%s\n' "$pinned_install" >"$fixture/docs/BETA.md"
if "$fixture/scripts/check-docs.sh" >/dev/null 2>&1; then
  fail "missing headless login guidance is rejected"
else
  pass "missing headless login guidance is rejected"
fi
printf '%s\n%s\n' "$pinned_install" "$headless_login" >"$fixture/docs/BETA.md"

sed 's/^name: wavo-cli$/name: altered/' \
  "$fixture/skills/wavo-cli/SKILL.md" \
  >"$fixture/skills/wavo-cli/SKILL.md.invalid"
mv "$fixture/skills/wavo-cli/SKILL.md.invalid" \
  "$fixture/skills/wavo-cli/SKILL.md"
if "$fixture/scripts/check-docs.sh" >/dev/null 2>&1; then
  fail "changed skill frontmatter is rejected"
else
  pass "changed skill frontmatter is rejected"
fi
cp "$repo_root/skills/wavo-cli/SKILL.md" \
  "$fixture/skills/wavo-cli/SKILL.md"

mv "$fixture/skills/wavo-cli/references/raw-api.md" \
  "$fixture/skills/wavo-cli/references/raw-api.md.missing"
if "$fixture/scripts/check-docs.sh" >/dev/null 2>&1; then
  fail "missing packaged skill reference is rejected"
else
  pass "missing packaged skill reference is rejected"
fi
mv "$fixture/skills/wavo-cli/references/raw-api.md.missing" \
  "$fixture/skills/wavo-cli/references/raw-api.md"

printf '%s\n' '# Orphan' >"$fixture/skills/wavo-cli/references/orphan.md"
if "$fixture/scripts/check-docs.sh" >/dev/null 2>&1; then
  fail "unlinked packaged skill reference is rejected"
else
  pass "unlinked packaged skill reference is rejected"
fi
rm "$fixture/skills/wavo-cli/references/orphan.md"

printf '%s\n' \
  '# Raw API rules' \
  '' \
  'Read [missing detail](missing.md).' \
  >"$fixture/skills/wavo-cli/references/raw-api.md"
if "$fixture/scripts/check-docs.sh" >/dev/null 2>&1; then
  fail "missing link inside packaged reference is rejected"
else
  pass "missing link inside packaged reference is rejected"
fi
