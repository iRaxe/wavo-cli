#!/bin/sh
set -eu
PATH=/usr/bin:/bin
export PATH

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd)
# shellcheck source=scripts/safe-git.sh
. "$script_dir/safe-git.sh"
repo_root=$(safe_git -C "$script_dir/.." rev-parse --show-toplevel)
scanner="$repo_root/scripts/check-branding.sh"
temporary_dir=$(mktemp -d)
root_probe="branding-probe-$$.txt"
sdk_probe="internal/sdk/testdata/branding-probe-$$.txt"
path_probe_root="branding-path-probe-$$"
upstream_marker='Chat'"woot"
upstream_component='chat'"woot"
path_probe="$path_probe_root/$upstream_component/clean.txt"
clean_symlink="branding symlink probe $$ clean"
forbidden_symlink="branding symlink probe $$ forbidden"
broken_symlink="branding symlink probe $$ broken"
pattern='chat'"woot"
repo_workspace_file="$repo_root/go.work"
repo_parent=$(CDPATH='' cd "$repo_root/.." && pwd -P)
parent_workspace_file="$repo_parent/go.work"
repo_workspace_created=0
parent_workspace_created=0

cleanup() {
  cleanup_original_status=$?
  trap - EXIT HUP INT TERM
  set +e
  cleanup_failed=0
  safe_git -C "$repo_root" update-index --force-remove -- \
    "$root_probe" "$sdk_probe" "$path_probe" \
    "$clean_symlink" "$forbidden_symlink" "$broken_symlink" >/dev/null 2>&1 || cleanup_failed=1
  rm -f "$repo_root/$root_probe" "$repo_root/$sdk_probe" \
    "$repo_root/$clean_symlink" "$repo_root/$forbidden_symlink" "$repo_root/$broken_symlink" || cleanup_failed=1
  if [ "$repo_workspace_created" -eq 1 ]; then
    rm -f "$repo_workspace_file" || cleanup_failed=1
  fi
  if [ "$parent_workspace_created" -eq 1 ]; then
    rm -f "$parent_workspace_file" || cleanup_failed=1
  fi
  rm -rf "$repo_root/${path_probe_root:?}" || cleanup_failed=1
  if [ -d "$temporary_dir" ]; then
    chmod -R u+w "$temporary_dir" 2>/dev/null || true
    rm -rf "$temporary_dir" || cleanup_failed=1
  fi
  if [ -e "$temporary_dir" ]; then
    cleanup_failed=1
  fi
  if [ "$cleanup_failed" -ne 0 ]; then
    printf '%s\n' "not ok - branding harness failed to remove its private fixtures" >&2
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

pass() {
  printf 'ok - %s\n' "$1"
}

fail() {
  printf 'not ok - %s\n' "$1" >&2
  exit 1
}

expect_success() {
  description=$1
  shift
  if "$@" >"$temporary_dir/stdout" 2>"$temporary_dir/stderr"; then
    pass "$description"
  else
    fail "$description"
  fi
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

run_scanner_from() {
  directory=$1
  shift
  (cd "$directory" && "$scanner" "$@")
}

expect_success "repository root invocation passes" run_scanner_from "$repo_root"
expect_success "nested repository invocation passes" run_scanner_from "$repo_root/internal"
expect_success "unrelated cwd invocation passes" run_scanner_from "$temporary_dir"

if [ -e "$repo_workspace_file" ] || [ -e "$parent_workspace_file" ]; then
  fail "workspace isolation probes require no pre-existing repository or parent go.work"
fi
workspace_replacement="$temporary_dir/local workspace replacement"
mkdir -p "$workspace_replacement"
printf '%s\n' 'module github.com/getkin/kin-openapi' >"$workspace_replacement/go.mod"

printf '%s\n' \
  'go 1.26.8' \
  'use .' \
  "replace github.com/getkin/kin-openapi => \"$workspace_replacement\"" \
  >"$repo_workspace_file"
repo_workspace_created=1
expect_success "repository-local Go workspace is ignored" "$scanner"
rm -f "$repo_workspace_file"
repo_workspace_created=0

printf '%s\n' \
  'go 1.26.8' \
  'use ./wavo-cli' \
  "replace github.com/getkin/kin-openapi => \"$workspace_replacement\"" \
  >"$parent_workspace_file"
parent_workspace_created=1
expect_success "enclosing parent Go workspace is ignored" "$scanner"
rm -f "$parent_workspace_file"
parent_workspace_created=0

caller_workspace="$temporary_dir/caller workspace.work"
printf '%s\n' \
  'go 1.26.8' \
  "use \"$repo_root\"" \
  "replace github.com/getkin/kin-openapi => \"$workspace_replacement\"" \
  >"$caller_workspace"
expect_success "caller GOWORK is ignored" env GOWORK="$caller_workspace" "$scanner"

copied_scanner="$temporary_dir/check-branding.sh"
cp "$scanner" "$copied_scanner"
cp "$repo_root/scripts/safe-git.sh" "$temporary_dir/safe-git.sh"
chmod +x "$copied_scanner"
expect_failure "scanner copy outside repository is rejected" 2 "Wavo branding scan failed to locate its repository." "$copied_scanner"

if safe_git -C "$repo_root" grep -q -i -E "$pattern" -- internal/sdk/testdata/application_swagger.json; then
  expect_success "exact Swagger snapshot is allowed" "$scanner"
else
  fail "exact Swagger snapshot is allowed"
fi

spoof_repo="$temporary_dir/clean alternate repository"
mkdir -p "$spoof_repo"
safe_git init -q "$spoof_repo"
safe_git -C "$spoof_repo" config user.name "Wavo Test"
safe_git -C "$spoof_repo" config user.email "wavo-test@example.invalid"
printf '%s\n' "clean alternate repository" >"$spoof_repo/clean.txt"
safe_git -C "$spoof_repo" add clean.txt
safe_git -C "$spoof_repo" commit -q -m "clean fixture"

printf '%s\n' "$upstream_marker" >"$repo_root/$root_probe"
safe_git -C "$repo_root" add -- "$root_probe"
expect_failure "GIT_DIR and GIT_WORK_TREE cannot hide forbidden content" 1 "Forbidden upstream branding in tracked content:" \
  env GIT_DIR="$spoof_repo/.git" GIT_WORK_TREE="$spoof_repo" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$root_probe"
rm -f "$repo_root/$root_probe"

mkdir -p "$repo_root/$(dirname "$path_probe")"
printf '%s\n' "clean path probe" >"$repo_root/$path_probe"
safe_git -C "$repo_root" add -- "$path_probe"
expect_failure "GIT_DIR and GIT_WORK_TREE cannot hide a forbidden path" 1 "Forbidden upstream branding in tracked paths:" \
  env GIT_DIR="$spoof_repo/.git" GIT_WORK_TREE="$spoof_repo" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$path_probe"
rm -rf "$repo_root/${path_probe_root:?}"

broken_target="$temporary_dir/$upstream_marker spoofed missing target"
ln -s "$broken_target" "$repo_root/$broken_symlink"
safe_git -C "$repo_root" add -- "$broken_symlink"
expect_failure "GIT_DIR and GIT_WORK_TREE cannot hide a forbidden symlink target" 1 "Forbidden upstream branding in tracked symlink targets:" \
  env GIT_DIR="$spoof_repo/.git" GIT_WORK_TREE="$spoof_repo" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$broken_symlink"
rm -f "$repo_root/$broken_symlink"

alternate_index="$temporary_dir/clean alternate index"
env -i PATH="$safe_system_path" LC_ALL=C GIT_INDEX_FILE="$alternate_index" "$safe_git_executable" -C "$repo_root" read-tree HEAD
printf '%s\n' "$upstream_marker" >"$repo_root/$root_probe"
safe_git -C "$repo_root" add -- "$root_probe"
expect_failure "GIT_INDEX_FILE cannot hide forbidden content" 1 "Forbidden upstream branding in tracked content:" \
  env GIT_INDEX_FILE="$alternate_index" "$scanner"
expect_failure "object database overrides cannot hide forbidden content" 1 "Forbidden upstream branding in tracked content:" \
  env GIT_OBJECT_DIRECTORY="$spoof_repo/.git/objects" GIT_ALTERNATE_OBJECT_DIRECTORIES="$repo_root/.git/objects" "$scanner"
expect_failure "GIT_CONFIG_COUNT cannot redirect the scanner worktree" 1 "Forbidden upstream branding in tracked content:" \
  env GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.worktree GIT_CONFIG_VALUE_0="$spoof_repo" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$root_probe"
rm -f "$repo_root/$root_probe"

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

printf '%s\n' "$upstream_marker" >"$repo_root/$root_probe"
safe_git -C "$repo_root" add -- "$root_probe"
expect_failure "caller PATH tools cannot hide forbidden content" 1 "Forbidden upstream branding in tracked content:" \
  env PATH="$fake_tools:$PATH" "$scanner"
if find "$fake_markers" -type f -print | grep -q .; then
  fail "caller PATH tools are never invoked"
fi
pass "caller PATH tools are never invoked"
safe_git -C "$repo_root" update-index --force-remove -- "$root_probe"
rm -f "$repo_root/$root_probe"

printf '%s\n' "$upstream_marker" >"$repo_root/$root_probe"
safe_git -C "$repo_root" add -- "$root_probe"
expect_failure "tracked forbidden content is rejected" 1 "Forbidden upstream branding in tracked content:" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$root_probe"
rm -f "$repo_root/$root_probe"

printf '%s\n' "$upstream_marker" >"$repo_root/$sdk_probe"
safe_git -C "$repo_root" add -- "$sdk_probe"
expect_failure "forbidden Swagger sibling content is rejected" 1 "Forbidden upstream branding in tracked content:" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$sdk_probe"
rm -f "$repo_root/$sdk_probe"

mkdir -p "$repo_root/$(dirname "$path_probe")"
printf '%s\n' "clean path probe" >"$repo_root/$path_probe"
safe_git -C "$repo_root" add -- "$path_probe"
expect_failure "forbidden tracked path is rejected" 1 "Forbidden upstream branding in tracked paths:" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$path_probe"
rm -rf "$repo_root/${path_probe_root:?}"

clean_target="$temporary_dir/clean symlink target"
printf '%s\n' "clean symlink target" >"$clean_target"
ln -s "$clean_target" "$repo_root/$clean_symlink"
safe_git -C "$repo_root" add -- "$clean_symlink"
expect_success "clean tracked symlink target passes" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$clean_symlink"
rm -f "$repo_root/$clean_symlink"

forbidden_target="$temporary_dir/$upstream_marker existing target"
printf '%s\n' "target exists" >"$forbidden_target"
ln -s "$forbidden_target" "$repo_root/$forbidden_symlink"
safe_git -C "$repo_root" add -- "$forbidden_symlink"
expect_failure "tracked symlink to forbidden existing target is rejected" 1 "Forbidden upstream branding in tracked symlink targets:" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$forbidden_symlink"
rm -f "$repo_root/$forbidden_symlink"

broken_target="$temporary_dir/$upstream_marker missing target"
ln -s "$broken_target" "$repo_root/$broken_symlink"
safe_git -C "$repo_root" add -- "$broken_symlink"
expect_failure "tracked broken symlink with forbidden target is rejected" 1 "Forbidden upstream branding in tracked symlink targets:" "$scanner"
safe_git -C "$repo_root" update-index --force-remove -- "$broken_symlink"
rm -f "$repo_root/$broken_symlink"

expect_failure "nonexistent binary is rejected" 2 "Binary branding target is not a regular file:" "$scanner" "$temporary_dir/missing-binary"
expect_failure "directory binary is rejected" 2 "Binary branding target is not a regular file:" "$scanner" "$temporary_dir"
expect_failure "more than one binary argument is rejected" 2 "Usage: scripts/check-branding.sh [binary]" "$scanner" "$temporary_dir/one" "$temporary_dir/two"

clean_binary="$temporary_dir/wavo"
clean_binary_context="$temporary_dir/clean binary Go context"
mkdir "$clean_binary_context"
chmod 0700 "$clean_binary_context"
safe_go "$clean_binary_context" -C "$repo_root" build -trimpath -o "$clean_binary" ./cmd/wavo
expect_success "clean compiled binary passes" "$scanner" "$clean_binary"
expect_success "clean binary scan ignores caller PATH tools" env PATH="$fake_tools:$PATH" "$scanner" "$clean_binary"
if find "$fake_markers" -type f -print | grep -q .; then
  fail "binary scan never invokes caller PATH tools"
fi
pass "binary scan never invokes caller PATH tools"

forbidden_binary="$temporary_dir/forbidden-binary"
printf 'safe-prefix-%s-safe-suffix\n' "$upstream_marker" >"$forbidden_binary"
expect_failure "binary marker is rejected" 1 "Compiled binary contains forbidden upstream branding:" "$scanner" "$forbidden_binary"

if find /tmp -maxdepth 1 -type d -name 'wavo-brand-scan.*' -print | grep -q .; then
  fail "scanner leaves no private Go contexts"
fi
pass "scanner leaves no private Go contexts"
