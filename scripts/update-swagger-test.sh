#!/bin/sh
set -eu
PATH=/usr/bin:/bin
export PATH

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd)
# shellcheck source=scripts/safe-git.sh
. "$script_dir/safe-git.sh"
repo_root=$(safe_git -C "$script_dir/.." rev-parse --show-toplevel)
updater="$repo_root/scripts/update-swagger.sh"
bundled_contract="$repo_root/internal/sdk/testdata/application_swagger.json"
temporary_dir=$(mktemp -d)

cleanup() {
  cleanup_original_status=$?
  trap - EXIT HUP INT TERM
  set +e
  cleanup_failed=0
  if [ -d "$temporary_dir" ]; then
    chmod -R u+w "$temporary_dir" 2>/dev/null || true
    rm -rf "$temporary_dir" || cleanup_failed=1
  fi
  if [ -e "$temporary_dir" ]; then
    cleanup_failed=1
  fi
  if [ "$cleanup_failed" -ne 0 ]; then
    printf '%s\n' "not ok - updater harness failed to remove its private fixtures" >&2
  fi
  if [ "$cleanup_original_status" -ne 0 ]; then
    exit "$cleanup_original_status"
  fi
  if [ "$cleanup_failed" -ne 0 ]; then
    exit 1
  fi
  exit 0
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM

readonly_cleanup_fixture="$temporary_dir/read-only cleanup fixture/nested"
mkdir -p "$readonly_cleanup_fixture"
printf '%s\n' 'cleanup fixture' >"$readonly_cleanup_fixture/file"
chmod 0444 "$readonly_cleanup_fixture/file"
chmod 0555 "$readonly_cleanup_fixture" "$(dirname "$readonly_cleanup_fixture")"

fail() {
  printf 'not ok - %s\n' "$1" >&2
  exit 1
}

pass() {
  printf 'ok - %s\n' "$1"
}

expect_failure() {
  description=$1
  expected_status=$2
  expected_stderr=$3
  shift 3
  set +e
  "$@" >"$temporary_dir/stdout" 2>"$temporary_dir/stderr"
  actual_status=$?
  set -e
  if [ "$actual_status" -ne "$expected_status" ]; then
    fail "$description (exit $actual_status, expected $expected_status)"
  fi
  if ! grep -Fq "$expected_stderr" "$temporary_dir/stderr"; then
    fail "$description (missing expected diagnostic)"
  fi
  pass "$description"
}

expect_unchanged() {
  description=$1
  if ! cmp -s "$baseline" "$destination"; then
    fail "$description"
  fi
  if ! cmp -s "$source_baseline" "$source_destination"; then
    fail "$description (provenance changed)"
  fi
  pass "$description"
}

init_source_repo() {
  directory=$1
  remote_url=$2
  safe_git init -q "$directory"
  safe_git -C "$directory" config user.name "Wavo Test"
  safe_git -C "$directory" config user.email "wavo-test@example.invalid"
  safe_git -C "$directory" remote add origin "$remote_url"
}

workspace="$temporary_dir/workspace with spaces"
destination="$workspace/internal/sdk/testdata/application_swagger.json"
source_destination="$workspace/internal/sdk/testdata/WAVO_SWAGGER_SOURCE"
baseline="$temporary_dir/original destination"
source_baseline="$temporary_dir/original provenance"
mkdir -p "$workspace/scripts" "$(dirname "$destination")"
cp "$updater" "$workspace/scripts/update-swagger.sh"
cp "$repo_root/scripts/safe-git.sh" "$workspace/scripts/safe-git.sh"
chmod +x "$workspace/scripts/update-swagger.sh"
copied_updater="$workspace/scripts/update-swagger.sh"
printf '%s\n' '{"preserve":"this destination"}' >"$destination"
printf '%s\n' 'mywavo 0000000000000000000000000000000000000000' >"$source_destination"
cp "$destination" "$baseline"
cp "$source_destination" "$source_baseline"

expect_failure "missing source directory is rejected" 2 "WAVO_SOURCE_DIR is required" env WAVO_SOURCE_DIR='' "$copied_updater"
expect_unchanged "missing source directory preserves destination"

# The updater executes the focused validator from its own checkout. Copy only
# the module inputs needed by the isolated workspace; the real snapshot is
# never selected as the updater destination.
mkdir -p "$workspace/cmd/wavo-contract-validator" "$workspace/internal/swaggercontract"
cp "$repo_root/go.mod" "$repo_root/go.sum" "$workspace/"
cp "$repo_root/cmd/wavo-contract-validator/main.go" "$workspace/cmd/wavo-contract-validator/"
cp "$repo_root/internal/swaggercontract/validate.go" "$workspace/internal/swaggercontract/"

non_git_source="$temporary_dir/non-Git source"
mkdir -p "$non_git_source/swagger/tag_groups"
cp "$bundled_contract" "$non_git_source/swagger/tag_groups/application_swagger.json"
expect_failure "non-Git source is rejected" 2 "WAVO_SOURCE_DIR must be the exact Git toplevel" env WAVO_SOURCE_DIR="$non_git_source" "$copied_updater"
expect_unchanged "non-Git source preserves destination"

external_canonical="$temporary_dir/external canonical repository"
init_source_repo "$external_canonical" "https://github.com/iRaxe/myWavo.git"
expect_failure "external GIT_DIR and GIT_WORK_TREE cannot bless a non-Git source" 2 "WAVO_SOURCE_DIR must be the exact Git toplevel" \
  env GIT_DIR="$external_canonical/.git" GIT_WORK_TREE="$non_git_source" WAVO_SOURCE_DIR="$non_git_source" "$copied_updater"
expect_unchanged "spoofed non-Git source preserves destination"

wrong_remote_source="$temporary_dir/wrong remote source"
init_source_repo "$wrong_remote_source" "https://github.com/example/not-wavo.git"
mkdir -p "$wrong_remote_source/swagger/tag_groups"
cp "$bundled_contract" "$wrong_remote_source/swagger/tag_groups/application_swagger.json"
expect_failure "wrong remote is rejected" 2 "Wavo source checkout has no canonical repository remote" env WAVO_SOURCE_DIR="$wrong_remote_source" "$copied_updater"
expect_unchanged "wrong remote preserves destination"
expect_failure "GIT_CONFIG_COUNT cannot inject a canonical remote" 2 "Wavo source checkout has no canonical repository remote" \
  env GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=remote.injected.url GIT_CONFIG_VALUE_0=https://github.com/iRaxe/myWavo.git \
  WAVO_SOURCE_DIR="$wrong_remote_source" "$copied_updater"
expect_unchanged "injected canonical remote preserves destination"

missing_head_source="$temporary_dir/missing HEAD source"
init_source_repo "$missing_head_source" "https://github.com/iRaxe/myWavo.git"
expect_failure "missing source HEAD is rejected" 2 "source HEAD must be a lowercase 40-hex commit SHA" \
  env WAVO_SOURCE_DIR="$missing_head_source" "$copied_updater"
expect_unchanged "missing source HEAD preserves both destinations"

sha256_source="$temporary_dir/SHA-256 source"
safe_git init -q --object-format=sha256 "$sha256_source"
safe_git -C "$sha256_source" config user.name "Wavo Test"
safe_git -C "$sha256_source" config user.email "wavo-test@example.invalid"
safe_git -C "$sha256_source" remote add origin "https://github.com/iRaxe/myWavo.git"
mkdir -p "$sha256_source/swagger/tag_groups"
cp "$bundled_contract" "$sha256_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$sha256_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$sha256_source" commit -q -m "SHA-256 fixture"
expect_failure "non-40-hex source commit is rejected" 2 "source HEAD must be a lowercase 40-hex commit SHA" \
  env WAVO_SOURCE_DIR="$sha256_source" "$copied_updater"
expect_unchanged "non-40-hex source preserves both destinations"

mkdir -p "$wrong_remote_source/nested source"
expect_failure "nested source directory is rejected" 2 "WAVO_SOURCE_DIR must be the exact Git toplevel" env WAVO_SOURCE_DIR="$wrong_remote_source/nested source" "$copied_updater"
expect_unchanged "nested source directory preserves destination"

missing_source="$temporary_dir/missing contract source"
init_source_repo "$missing_source" "https://github.com/iRaxe/myWavo.git"
safe_git -C "$missing_source" commit -q --allow-empty -m "empty fixture"
expect_failure "missing source contract is rejected" 2 "Wavo Swagger source is not a readable regular file" env WAVO_SOURCE_DIR="$missing_source" "$copied_updater"
expect_unchanged "missing source contract preserves destination"

external_contract="$temporary_dir/external valid application_swagger.json"
cp "$bundled_contract" "$external_contract"
file_symlink_source="$temporary_dir/file symlink source"
init_source_repo "$file_symlink_source" "https://github.com/iRaxe/myWavo.git"
mkdir -p "$file_symlink_source/swagger/tag_groups"
ln -s "$external_contract" "$file_symlink_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$file_symlink_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$file_symlink_source" commit -q -m "symlink fixture"
expect_failure "external contract file symlink is rejected" 2 "Wavo Swagger source path must not be a symlink" \
  env WAVO_SOURCE_DIR="$file_symlink_source" "$copied_updater"
expect_unchanged "file symlink source preserves destination"

external_parent="$temporary_dir/external contract parent"
mkdir -p "$external_parent"
cp "$bundled_contract" "$external_parent/application_swagger.json"
parent_symlink_source="$temporary_dir/parent symlink source"
init_source_repo "$parent_symlink_source" "https://github.com/iRaxe/myWavo.git"
mkdir -p "$parent_symlink_source/swagger"
ln -s "$external_parent" "$parent_symlink_source/swagger/tag_groups"
safe_git -C "$parent_symlink_source" add swagger/tag_groups
safe_git -C "$parent_symlink_source" commit -q -m "parent symlink fixture"
expect_failure "external contract parent symlink is rejected" 2 "Wavo Swagger source path escapes the Wavo source checkout" \
  env WAVO_SOURCE_DIR="$parent_symlink_source" "$copied_updater"
expect_unchanged "parent symlink source preserves destination"

untracked_source="$temporary_dir/untracked contract source"
init_source_repo "$untracked_source" "https://github.com/iRaxe/myWavo.git"
mkdir -p "$untracked_source/swagger/tag_groups"
cp "$bundled_contract" "$untracked_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$untracked_source" commit -q --allow-empty -m "untracked fixture"
expect_failure "untracked regular contract is rejected" 2 "Wavo Swagger source must be tracked as a regular blob" \
  env WAVO_SOURCE_DIR="$untracked_source" "$copied_updater"
expect_unchanged "untracked source preserves destination"

staged_source="$temporary_dir/staged modified source"
init_source_repo "$staged_source" "https://github.com/iRaxe/myWavo.git"
mkdir -p "$staged_source/swagger/tag_groups"
cp "$bundled_contract" "$staged_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$staged_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$staged_source" commit -q -m "contract fixture"
printf '\n' >>"$staged_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$staged_source" add swagger/tag_groups/application_swagger.json
expect_failure "staged modified contract is rejected" 2 "Wavo Swagger source must have no staged or unstaged changes" \
  env WAVO_SOURCE_DIR="$staged_source" "$copied_updater"
expect_unchanged "staged modified source preserves destination"

unstaged_source="$temporary_dir/unstaged modified source"
init_source_repo "$unstaged_source" "https://github.com/iRaxe/myWavo.git"
mkdir -p "$unstaged_source/swagger/tag_groups"
cp "$bundled_contract" "$unstaged_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$unstaged_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$unstaged_source" commit -q -m "contract fixture"
printf '\n' >>"$unstaged_source/swagger/tag_groups/application_swagger.json"
expect_failure "unstaged modified contract is rejected" 2 "Wavo Swagger source must have no staged or unstaged changes" \
  env WAVO_SOURCE_DIR="$unstaged_source" "$copied_updater"
expect_unchanged "unstaged modified source preserves destination"

invalid_source="$temporary_dir/invalid contract source"
init_source_repo "$invalid_source" "git@github.com:iRaxe/myWavo.git"
mkdir -p "$invalid_source/swagger/tag_groups"
printf '%s\n' '{not valid JSON' >"$invalid_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$invalid_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$invalid_source" commit -q -m "invalid contract fixture"
expect_failure "invalid contract is rejected" 2 "Wavo Swagger contract validation failed" env WAVO_SOURCE_DIR="$invalid_source" "$copied_updater"
expect_unchanged "invalid contract preserves destination byte-for-byte"

# Seed a caller-controlled module cache through the same isolated automatic
# toolchain and checksum policy used by production validation, then modify the
# exact dependency used by the validator. The updater must not execute this
# injected package or write anything else to the poisoned caches.
poison_context="$temporary_dir/poison fixture context"
mkdir -m 0700 "$poison_context"
safe_go "$poison_context" -C "$temporary_dir" mod download github.com/getkin/kin-openapi@v0.145.0
poison_gopath="$poison_context/gopath"
poison_modcache="$poison_gopath/pkg/mod"
poison_module="$poison_modcache/github.com/getkin/kin-openapi@v0.145.0"
if [ ! -d "$poison_module" ]; then
  fail "poison module fixture is prepared with automatic trusted toolchain acquisition"
fi
pass "poison module fixture is prepared with automatic trusted toolchain acquisition"
poison_marker="$temporary_dir/poisoned-kin-openapi-invoked"
chmod -R u+w "$poison_module"
printf '%s\n' \
  'package openapi3' \
  'import "os"' \
  "func init() { _ = os.WriteFile(\"$poison_marker\", []byte(\"invoked\"), 0600) }" \
  >"$poison_module/openapi3/wavo_poison.go"

workspace_replacement="$temporary_dir/workspace-replacement"
ln -s "$poison_module" "$workspace_replacement"

printf '%s\n' \
  'go 1.26.6' \
  'use .' \
  "replace github.com/getkin/kin-openapi => $workspace_replacement" \
  >"$workspace/go.work"
expect_failure "repository-local Go workspace is ignored by updater" 2 "Wavo Swagger contract validation failed" \
  env WAVO_SOURCE_DIR="$invalid_source" "$copied_updater"
if ! grep -Fq "load OpenAPI document" "$temporary_dir/stderr"; then
  fail "repository-local workspace still uses the real validator"
fi
if [ -e "$poison_marker" ]; then
  fail "repository-local workspace replacement is never invoked"
fi
pass "repository-local workspace replacement is never invoked"
expect_unchanged "repository-local workspace preserves destination"
rm -f "$workspace/go.work"

printf '%s\n' \
  'go 1.26.6' \
  'use "./workspace with spaces"' \
  "replace github.com/getkin/kin-openapi => $workspace_replacement" \
  >"$temporary_dir/go.work"
expect_failure "enclosing parent Go workspace is ignored by updater" 2 "Wavo Swagger contract validation failed" \
  env WAVO_SOURCE_DIR="$invalid_source" "$copied_updater"
if ! grep -Fq "load OpenAPI document" "$temporary_dir/stderr"; then
  fail "enclosing workspace still uses the real validator"
fi
if [ -e "$poison_marker" ]; then
  fail "enclosing workspace replacement is never invoked"
fi
pass "enclosing workspace replacement is never invoked"
expect_unchanged "enclosing workspace preserves destination"
rm -f "$temporary_dir/go.work"

caller_workspace="$temporary_dir/caller-workspace.work"
printf '%s\n' \
  'go 1.26.6' \
  'use "./workspace with spaces"' \
  "replace github.com/getkin/kin-openapi => $workspace_replacement" \
  >"$caller_workspace"
expect_failure "caller GOWORK is ignored by updater" 2 "Wavo Swagger contract validation failed" \
  env GOWORK="$caller_workspace" WAVO_SOURCE_DIR="$invalid_source" "$copied_updater"
if ! grep -Fq "load OpenAPI document" "$temporary_dir/stderr"; then
  fail "caller workspace still uses the real validator"
fi
if [ -e "$poison_marker" ]; then
  fail "caller workspace replacement is never invoked"
fi
pass "caller workspace replacement is never invoked"
expect_unchanged "caller workspace preserves destination"

poison_goenv="$temporary_dir/poison goenv"
printf '%s\n' 'GOFLAGS=-trimpath' >"$poison_goenv"
trusted_goroot=$(safe_go "$poison_context" -C "$temporary_dir" env GOROOT)
poison_goroot="$temporary_dir/poison goroot"
ln -s "$trusted_goroot" "$poison_goroot"
expect_failure "caller Go environment cannot influence validation" 2 "Wavo Swagger contract validation failed" \
  env \
  GOMODCACHE="$poison_modcache" \
  GOCACHE="$temporary_dir/poison fixture build cache" \
  GOPATH="$poison_gopath" \
  GOROOT="$poison_goroot" \
  GOENV="$poison_goenv" \
  GOFLAGS=-trimpath \
  GOTOOLCHAIN=auto \
  HOME="$temporary_dir/poison home" \
  XDG_CONFIG_HOME="$temporary_dir/poison xdg config" \
  XDG_CACHE_HOME="$temporary_dir/poison xdg cache" \
  WAVO_SOURCE_DIR="$invalid_source" \
  "$copied_updater"
if ! grep -Fq "load OpenAPI document" "$temporary_dir/stderr"; then
  fail "invalid contract is rejected by the isolated validator"
fi
pass "invalid contract is rejected by the isolated validator"
if [ -e "$poison_marker" ]; then
  fail "poisoned validator dependency is never invoked"
fi
pass "poisoned validator dependency is never invoked"
expect_unchanged "poisoned Go environment preserves destination"

fake_tools="$temporary_dir/fake tools"
fake_markers="$temporary_dir/fake tool markers"
mkdir -p "$fake_tools" "$fake_markers"
for fake_tool in git go grep strings dirname; do
  printf '%s\n' \
    '#!/bin/sh' \
    "printf '%s\\n' invoked >'$fake_markers/$fake_tool'" \
    'exit 99' >"$fake_tools/$fake_tool"
  chmod +x "$fake_tools/$fake_tool"
done
expect_failure "caller PATH tools cannot validate an invalid source" 2 "Wavo Swagger contract validation failed" \
  env PATH="$fake_tools:$PATH" WAVO_SOURCE_DIR="$invalid_source" "$copied_updater"
if find "$fake_markers" -type f -print | grep -q .; then
  fail "updater never invokes caller PATH tools"
fi
pass "updater never invokes caller PATH tools"
expect_unchanged "fake PATH invalid source preserves destination"

primary_source="$temporary_dir/primary source repo"
valid_source="$temporary_dir/valid linked worktree with spaces"
init_source_repo "$primary_source" "git@github.com:iRaxe/myWavo.git"
mkdir -p "$primary_source/swagger/tag_groups"
cp "$bundled_contract" "$primary_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$primary_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$primary_source" commit -q -m "fixture"
safe_git -C "$primary_source" worktree add -q --detach "$valid_source" HEAD
captured_source_sha=$(safe_git -C "$valid_source" rev-parse HEAD)

# Move the source checkout to a different clean commit immediately after the
# updater captures commit A. Looking up the tree through mutable HEAD would
# pair A's provenance with B's bytes; immutable lookup must instead fail closed.
printf '\n' >>"$primary_source/swagger/tag_groups/application_swagger.json"
safe_git -C "$primary_source" add swagger/tag_groups/application_swagger.json
safe_git -C "$primary_source" commit -q -m "moved HEAD fixture"
moved_source_sha=$(safe_git -C "$primary_source" rev-parse HEAD)
race_updater="$workspace/scripts/update-swagger-head-race.sh"
sed "s|# All subsequent commit-tree reads must use this captured immutable object ID.|safe_git -C \"\$source_root\" reset --hard '$moved_source_sha' >/dev/null|" \
  "$updater" >"$race_updater"
chmod +x "$race_updater"
expect_failure "source HEAD movement cannot split contract provenance" 2 \
  "Wavo Swagger source bytes must match the recorded source commit" \
  env WAVO_SOURCE_DIR="$valid_source" "$race_updater"
expect_unchanged "source HEAD movement preserves both destinations"
safe_git -C "$valid_source" reset --hard "$captured_source_sha" >/dev/null

# A private copy with the production proxy constant replaced proves that
# dependency/toolchain acquisition failure is terminal before destination IO.
cp "$workspace/scripts/safe-git.sh" "$temporary_dir/safe-git.production.sh"
sed 's#https://proxy.golang.org,direct#http://127.0.0.1:1#' \
  "$temporary_dir/safe-git.production.sh" >"$workspace/scripts/safe-git.sh"
expect_failure "trusted Go acquisition failure is rejected" 2 "Wavo Swagger validator build failed" \
  env WAVO_SOURCE_DIR="$valid_source" "$copied_updater"
expect_unchanged "trusted Go acquisition failure preserves destination"
cp "$temporary_dir/safe-git.production.sh" "$workspace/scripts/safe-git.sh"

env WAVO_SOURCE_DIR="$valid_source" "$copied_updater" >"$temporary_dir/stdout" 2>"$temporary_dir/stderr"
if ! cmp -s "$bundled_contract" "$destination"; then
  fail "valid linked-worktree contract is copied"
fi
pass "valid linked-worktree contract is copied"
expected_source_sha=$(safe_git -C "$valid_source" rev-parse HEAD)
if [ "$(cat "$source_destination")" != "mywavo $expected_source_sha" ]; then
  fail "valid linked-worktree provenance is copied"
fi
pass "valid linked-worktree provenance is copied"
if [ "$(find "$destination" -prune -perm 0644 -print)" != "$destination" ]; then
  fail "updated contract mode is 0644"
fi
pass "updated contract mode is 0644"

env GIT_INDEX_FILE="$temporary_dir/spoof index" \
  GIT_OBJECT_DIRECTORY="$temporary_dir/spoof objects" \
  GIT_ALTERNATE_OBJECT_DIRECTORIES="$primary_source/.git/objects" \
  WAVO_SOURCE_DIR="$valid_source" "$copied_updater" >"$temporary_dir/stdout" 2>"$temporary_dir/stderr"
if ! cmp -s "$bundled_contract" "$destination"; then
  fail "Git index and object overrides are ignored"
fi
pass "Git index and object overrides are ignored"
if [ "$(cat "$source_destination")" != "mywavo $expected_source_sha" ]; then
  fail "Git overrides cannot influence source provenance"
fi
pass "Git overrides cannot influence source provenance"

if find "$(dirname "$destination")" -name '.application_swagger.json.tmp.*' -print | grep -q .; then
  fail "updater leaves no temporary files"
fi
pass "updater leaves no temporary files"

if find /tmp -maxdepth 1 -type d -name 'wavo-swagger-validation.*' -print | grep -q .; then
  fail "updater leaves no private Go contexts"
fi
pass "updater leaves no private Go contexts"
