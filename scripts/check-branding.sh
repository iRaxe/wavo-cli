#!/bin/sh
set -eu
PATH=/usr/bin:/bin
export PATH

fail_scan() {
  printf '%s\n' "$1" >&2
  exit 2
}

script_path=$0
case "$script_path" in
  */*) ;;
  *)
    set +e
    script_path=$(command -v "$script_path")
    command_status=$?
    set -e
    if [ "$command_status" -ne 0 ] || [ -z "$script_path" ]; then
      fail_scan "Wavo branding scan failed to locate its script."
    fi
    ;;
esac

set +e
script_dir=$(CDPATH='' cd "$(dirname "$script_path")" && pwd -P)
directory_status=$?
set -e
if [ "$directory_status" -ne 0 ] || [ -z "$script_dir" ]; then
  fail_scan "Wavo branding scan failed to locate its script directory."
fi

# shellcheck source=scripts/safe-git.sh
. "$script_dir/safe-git.sh"

scan_validation_root=''
cleanup() {
  if [ -n "$scan_validation_root" ]; then
    case "$scan_validation_root" in
      /tmp/wavo-brand-scan.*)
        if [ -d "$scan_validation_root" ]; then
          rm -rf "$scan_validation_root"
        fi
        ;;
    esac
  fi
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM

set +e
repo_root=$(safe_git -C "$script_dir/.." rev-parse --show-toplevel 2>/dev/null)
repository_status=$?
set -e
if [ "$repository_status" -ne 0 ] || [ -z "$repo_root" ]; then
  fail_scan "Wavo branding scan failed to locate its repository."
fi
set +e
repo_root=$(CDPATH='' cd "$repo_root" 2>/dev/null && pwd -P)
repository_directory_status=$?
set -e
if [ "$repository_directory_status" -ne 0 ] || [ -z "$repo_root" ]; then
  fail_scan "Wavo branding scan failed to resolve its repository physically."
fi
if [ -L "$repo_root/.git" ] || { [ ! -d "$repo_root/.git" ] && [ ! -f "$repo_root/.git" ]; }; then
  fail_scan "Wavo branding scan rejected a repository without physical Git metadata."
fi

expected_scanner="$repo_root/scripts/check-branding.sh"
if [ "$script_dir/check-branding.sh" != "$expected_scanner" ] || [ ! -f "$expected_scanner" ]; then
  fail_scan "Wavo branding scan rejected a script outside its repository path."
fi

set +e
resolved_toplevel=$(safe_git -C "$repo_root" rev-parse --show-toplevel 2>/dev/null)
resolved_toplevel_status=$?
resolved_git_dir=$(safe_git -C "$repo_root" rev-parse --absolute-git-dir 2>/dev/null)
resolved_git_dir_status=$?
resolved_index=$(safe_git -C "$repo_root" rev-parse --path-format=absolute --git-path index 2>/dev/null)
resolved_index_status=$?
safe_git -C "$repo_root" ls-files --error-unmatch scripts/check-branding.sh >/dev/null 2>&1
tracked_scanner_status=$?
set -e
if [ "$resolved_toplevel_status" -ne 0 ] || [ "$resolved_toplevel" != "$repo_root" ]; then
  fail_scan "Wavo branding scan rejected inconsistent repository toplevel metadata."
fi
if [ "$resolved_git_dir_status" -ne 0 ] || [ "$resolved_index_status" -ne 0 ] || \
  [ "$resolved_index" != "$resolved_git_dir/index" ] || [ -L "$resolved_index" ] || [ ! -f "$resolved_index" ]; then
  fail_scan "Wavo branding scan rejected inconsistent repository index metadata."
fi
if [ "$tracked_scanner_status" -ne 0 ]; then
  fail_scan "Wavo branding scan rejected an untracked scanner script."
fi

if [ "$#" -gt 1 ]; then
  printf '%s\n' "Usage: scripts/check-branding.sh [binary]" >&2
  exit 2
fi

set +e
tracked_paths=$(safe_git -C "$repo_root" ls-files)
paths_status=$?
set -e
if [ "$paths_status" -ne 0 ]; then
  fail_scan "Wavo branding scan failed to list tracked paths (git ls-files exit $paths_status)."
fi

set +e
path_matches=$(printf '%s\n' "$tracked_paths" | grep -i -E 'chatwoot|chwt\.app|CHATWOOT_')
path_match_status=$?
set -e
case "$path_match_status" in
  0)
    printf '%s\n' "Forbidden upstream branding in tracked paths:" >&2
    printf '%s\n' "$path_matches" >&2
    exit 1
    ;;
  1) ;;
  *)
    fail_scan "Wavo branding scan failed while matching tracked paths (grep exit $path_match_status)."
    ;;
esac

set +e
content_matches=$(safe_git -C "$repo_root" grep -n -i -E 'chatwoot|chwt\.app|CHATWOOT_' -- \
  ':(top)' \
  ':(top,exclude)LICENSE' \
  ':(top,exclude)NOTICE.md' \
  ':(top,exclude)docs/UPSTREAM.md' \
  ':(top,exclude)internal/sdk/testdata/application_swagger.json' \
  ':(top,exclude)scripts/check-branding.sh')
content_status=$?
set -e
case "$content_status" in
  0)
    printf '%s\n' "Forbidden upstream branding in tracked content:" >&2
    printf '%s\n' "$content_matches" >&2
    exit 1
    ;;
  1) ;;
  *)
    fail_scan "Wavo branding scan failed while searching tracked content (git grep exit $content_status)."
    ;;
esac

set +e
scan_validation_root=$(mktemp -d /tmp/wavo-brand-scan.XXXXXX)
scan_context_status=$?
set -e
if [ "$scan_context_status" -ne 0 ] || [ -z "$scan_validation_root" ]; then
  fail_scan "Wavo branding scan failed to create a private helper context."
fi
if ! chmod 0700 "$scan_validation_root"; then
  fail_scan "Wavo branding scan failed to secure its private helper context."
fi
scan_helper_modfile="$scan_validation_root/helper.mod"
if ! printf '%s\n' 'module github.com/iRaxe/wavo-cli' >"$scan_helper_modfile"; then
  fail_scan "Wavo branding scan failed to create its private helper module."
fi
set +e
symlink_matches=$(safe_go_local "$scan_validation_root" -C "$repo_root" run \
  -modfile="$scan_helper_modfile" ./cmd/wavo-brandcheck-symlinks "$repo_root")
symlink_status=$?
set -e
if [ "$symlink_status" -ne 0 ]; then
  fail_scan "Wavo branding scan failed while executing the tracked symlink target helper (go exit $symlink_status)."
fi
case "$symlink_matches" in
  WavoSymlinkScanClean) ;;
  WavoSymlinkScanMatches:*)
    symlink_matches=${symlink_matches#WavoSymlinkScanMatches:}
    printf '%s\n' "Forbidden upstream branding in tracked symlink targets:" >&2
    printf '%s\n' "$symlink_matches" >&2
    exit 1
    ;;
  WavoSymlinkScanError)
    fail_scan "Wavo branding scan failed while inspecting tracked symlink targets."
    ;;
  *)
    fail_scan "Wavo branding scan received an invalid tracked symlink helper response."
    ;;
esac

if [ "$#" -eq 1 ]; then
  binary=$1
  binary_path=$binary
  case "$binary_path" in
    -*) binary_path="./$binary_path" ;;
  esac
  if [ ! -f "$binary_path" ]; then
    printf '%s\n' "Binary branding target is not a regular file: $binary" >&2
    exit 2
  fi
  if [ ! -r "$binary_path" ]; then
    printf '%s\n' "Binary branding target is not readable: $binary" >&2
    exit 2
  fi
  if ! command -v strings >/dev/null 2>&1; then
    printf '%s\n' "Wavo binary branding scan requires the strings command." >&2
    exit 2
  fi

  set +e
  strings_output=$(strings "$binary_path")
  strings_status=$?
  set -e
  if [ "$strings_status" -ne 0 ]; then
    printf '%s\n' "Wavo binary branding scan failed to inspect $binary (strings exit $strings_status)." >&2
    exit 2
  fi

  set +e
  printf '%s\n' "$strings_output" | grep -qi -E 'chatwoot|chwt\.app|CHATWOOT_'
  marker_status=$?
  set -e
  case "$marker_status" in
    0)
      printf '%s\n' "Compiled binary contains forbidden upstream branding: $binary" >&2
      exit 1
      ;;
    1) ;;
    *)
      fail_scan "Wavo binary branding scan failed while matching extracted strings (grep exit $marker_status)."
      ;;
  esac
fi
