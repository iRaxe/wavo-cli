#!/bin/sh
set -eu
PATH=/usr/bin:/bin
export PATH

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd -P)
repo_root=$(CDPATH='' cd "$script_dir/.." && pwd -P)
temporary_file=''
temporary_source_file=''
contract_backup=''
source_backup=''
replacement_started=0
validation_root=''
validator_binary=''

# shellcheck source=scripts/safe-git.sh
. "$script_dir/safe-git.sh"

cleanup() {
  cleanup_original_status=$?
  trap - EXIT HUP INT TERM
  set +e
  cleanup_failed=0
  if [ "$replacement_started" -ne 0 ]; then
    if [ -z "$contract_backup" ] || [ -z "$source_backup" ] || \
      ! cp "$contract_backup" "$destination" || \
      ! cp "$source_backup" "$source_destination"; then
      cleanup_failed=1
    fi
  fi
  for cleanup_file in "$temporary_file" "$temporary_source_file" "$contract_backup" "$source_backup"; do
    if [ -n "$cleanup_file" ] && [ -e "$cleanup_file" ]; then
      rm -f "$cleanup_file" || cleanup_failed=1
      if [ -e "$cleanup_file" ]; then
        cleanup_failed=1
      fi
    fi
  done
  if [ -n "$validation_root" ]; then
    case "$validation_root" in
      /tmp/wavo-swagger-validation.*)
        if [ -d "$validation_root" ]; then
          chmod -R u+w "$validation_root" 2>/dev/null || true
          rm -rf "$validation_root" || cleanup_failed=1
          if [ -e "$validation_root" ]; then
            cleanup_failed=1
          fi
        fi
        ;;
    esac
  fi
  if [ "$cleanup_failed" -ne 0 ]; then
    printf '%s\n' "Wavo Swagger refresh failed to remove its private validation files." >&2
  fi
  if [ "$cleanup_original_status" -ne 0 ]; then
    exit "$cleanup_original_status"
  fi
  if [ "$cleanup_failed" -ne 0 ]; then
    exit 2
  fi
  exit 0
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM

fail_update() {
  printf '%s\n' "$1" >&2
  exit 2
}

validate_contract() {
  contract_path=$1
  set +e
  env -i PATH="$safe_system_path" LC_ALL=C "$validator_binary" "$contract_path"
  validation_status=$?
  set -e
  if [ "$validation_status" -ne 0 ]; then
    fail_update "Wavo Swagger contract validation failed (validator exit $validation_status)."
  fi
}

if [ -z "${WAVO_SOURCE_DIR:-}" ]; then
  fail_update "WAVO_SOURCE_DIR is required to refresh the bundled Wavo Swagger contract."
fi

set +e
source_dir=$(CDPATH='' cd "$WAVO_SOURCE_DIR" 2>/dev/null && pwd -P)
source_directory_status=$?
set -e
if [ "$source_directory_status" -ne 0 ] || [ -z "$source_dir" ]; then
  fail_update "WAVO_SOURCE_DIR must be the exact Git toplevel of a Wavo source checkout."
fi
if [ -L "$source_dir/.git" ] || { [ ! -d "$source_dir/.git" ] && [ ! -f "$source_dir/.git" ]; }; then
  fail_update "WAVO_SOURCE_DIR must be the exact Git toplevel of a Wavo source checkout."
fi

set +e
git_root=$(safe_git -C "$source_dir" rev-parse --show-toplevel 2>/dev/null)
git_root_status=$?
set -e
if [ "$git_root_status" -ne 0 ] || [ -z "$git_root" ]; then
  fail_update "WAVO_SOURCE_DIR must be the exact Git toplevel of a Wavo source checkout."
fi
set +e
source_root=$(CDPATH='' cd "$git_root" 2>/dev/null && pwd -P)
source_root_status=$?
set -e
if [ "$source_root_status" -ne 0 ] || [ "$source_dir" != "$source_root" ]; then
  fail_update "WAVO_SOURCE_DIR must be the exact Git toplevel of a Wavo source checkout."
fi

set +e
remote_entries=$(safe_git -C "$source_root" config --local --get-regexp '^remote\..*\.url$' 2>/dev/null)
remote_status=$?
set -e
if [ "$remote_status" -gt 1 ]; then
  fail_update "Wavo Swagger refresh failed to inspect source repository remotes."
fi

canonical_remote=0
while IFS= read -r remote_entry; do
  [ -n "$remote_entry" ] || continue
  case "$remote_entry" in
    *' '*) remote_url=${remote_entry#* } ;;
    *) fail_update "Wavo Swagger refresh received malformed local remote configuration." ;;
  esac
  case "$remote_url" in
    https://github.com/iRaxe/myWavo | \
      https://github.com/iRaxe/myWavo.git | \
      git@github.com:iRaxe/myWavo | \
      git@github.com:iRaxe/myWavo.git | \
      ssh://git@github.com/iRaxe/myWavo | \
      ssh://git@github.com/iRaxe/myWavo.git)
      canonical_remote=1
      ;;
  esac
done <<EOF
$remote_entries
EOF
if [ "$canonical_remote" -ne 1 ]; then
  fail_update "Wavo source checkout has no canonical repository remote."
fi

set +e
source_sha=$(safe_git -C "$source_root" rev-parse --verify 'HEAD^{commit}' 2>/dev/null)
source_sha_status=$?
set -e
if [ "$source_sha_status" -ne 0 ] || ! printf '%s\n' "$source_sha" | grep -Eq '^[0-9a-f]{40}$'; then
  fail_update "Wavo Swagger source HEAD must be a lowercase 40-hex commit SHA."
fi
# All subsequent commit-tree reads must use this captured immutable object ID.

source_relative_path=swagger/tag_groups/application_swagger.json
source_contract="$source_root/$source_relative_path"
if [ -L "$source_contract" ]; then
  fail_update "Wavo Swagger source path must not be a symlink."
fi
set +e
source_parent=$(CDPATH='' cd "$(dirname "$source_contract")" 2>/dev/null && pwd -P)
source_parent_status=$?
set -e
if [ "$source_parent_status" -ne 0 ] || [ -z "$source_parent" ]; then
  fail_update "Wavo Swagger source is not a readable regular file: $source_contract"
fi
case "$source_parent/" in
  "$source_root/"*) ;;
  *) fail_update "Wavo Swagger source path escapes the Wavo source checkout." ;;
esac
physical_source_contract="$source_parent/$(basename "$source_contract")"
if [ -L "$physical_source_contract" ]; then
  fail_update "Wavo Swagger source path must not be a symlink."
fi
if [ ! -f "$physical_source_contract" ] || [ ! -r "$physical_source_contract" ]; then
  fail_update "Wavo Swagger source is not a readable regular file: $source_contract"
fi

set +e
index_entry=$(safe_git -C "$source_root" ls-files -s -- "$source_relative_path")
index_status=$?
set -e
if [ "$index_status" -ne 0 ] || [ -z "$index_entry" ]; then
  fail_update "Wavo Swagger source must be tracked as a regular blob."
fi
newline='
'
case "$index_entry" in
  *"$newline"*) fail_update "Wavo Swagger source must have exactly one tracked index entry." ;;
esac
IFS=' 	' read -r index_mode index_object index_stage index_path <<EOF
$index_entry
EOF
if { [ "$index_mode" != "100644" ] && [ "$index_mode" != "100755" ]; } || \
  [ "$index_stage" != "0" ] || [ "$index_path" != "$source_relative_path" ]; then
  fail_update "Wavo Swagger source must be tracked as a regular blob."
fi

set +e
safe_git -C "$source_root" diff --cached --quiet -- "$source_relative_path"
staged_status=$?
safe_git -C "$source_root" diff --quiet -- "$source_relative_path"
unstaged_status=$?
set -e
if [ "$staged_status" -eq 1 ] || [ "$unstaged_status" -eq 1 ]; then
  fail_update "Wavo Swagger source must have no staged or unstaged changes."
fi
if [ "$staged_status" -ne 0 ] || [ "$unstaged_status" -ne 0 ]; then
  fail_update "Wavo Swagger refresh failed to inspect source contract changes."
fi

set +e
source_object=$(safe_git -C "$source_root" hash-object -- "$physical_source_contract")
hash_status=$?
set -e
if [ "$hash_status" -ne 0 ] || [ -z "$source_object" ]; then
  fail_update "Wavo Swagger refresh failed to hash the source contract."
fi
if [ "$source_object" != "$index_object" ]; then
  fail_update "Wavo Swagger source bytes do not match the reviewed index blob."
fi

set +e
head_entry=$(safe_git -C "$source_root" ls-tree "$source_sha" -- "$source_relative_path")
head_entry_status=$?
set -e
if [ "$head_entry_status" -ne 0 ] || [ -z "$head_entry" ]; then
  fail_update "Wavo Swagger source must exist in the recorded source commit."
fi
case "$head_entry" in
  *"$newline"*) fail_update "Wavo Swagger source commit must have exactly one contract entry." ;;
esac
IFS=' 	' read -r head_mode head_type head_object head_path <<EOF
$head_entry
EOF
if { [ "$head_mode" != "100644" ] && [ "$head_mode" != "100755" ]; } || \
  [ "$head_type" != "blob" ] || [ "$head_object" != "$index_object" ] || \
  [ "$head_path" != "$source_relative_path" ]; then
  fail_update "Wavo Swagger source bytes must match the recorded source commit."
fi

# Build the focused validator once in a private validation home/cache. Go gets
# no caller environment, and acquisition failure occurs before destination IO.
set +e
validation_root=$(mktemp -d /tmp/wavo-swagger-validation.XXXXXX)
validation_root_status=$?
set -e
if [ "$validation_root_status" -ne 0 ] || [ -z "$validation_root" ]; then
  fail_update "Wavo Swagger refresh failed to create a private validation context."
fi
if ! chmod 0700 "$validation_root"; then
  fail_update "Wavo Swagger refresh failed to secure the private validation context."
fi
validator_binary="$validation_root/wavo-contract-validator"
set +e
safe_go "$validation_root" -C "$repo_root" build -o "$validator_binary" ./cmd/wavo-contract-validator
validator_build_status=$?
set -e
if [ "$validator_build_status" -ne 0 ] || [ ! -f "$validator_binary" ] || [ ! -x "$validator_binary" ]; then
  fail_update "Wavo Swagger validator build failed (go exit $validator_build_status)."
fi

# Validate the source before creating anything in the destination directory.
validate_contract "$physical_source_contract"

destination="$repo_root/internal/sdk/testdata/application_swagger.json"
destination_dir=$(dirname "$destination")
source_destination="$destination_dir/WAVO_SWAGGER_SOURCE"
if [ ! -d "$destination_dir" ]; then
  fail_update "Wavo Swagger destination directory does not exist: $destination_dir"
fi
if [ -L "$destination" ] || [ ! -f "$destination" ] || \
  [ -L "$source_destination" ] || [ ! -f "$source_destination" ]; then
  fail_update "Wavo Swagger contract and provenance destinations must be existing regular files."
fi

set +e
temporary_file=$(mktemp "$destination_dir/.application_swagger.json.tmp.XXXXXX")
temporary_status=$?
set -e
if [ "$temporary_status" -ne 0 ] || [ -z "$temporary_file" ]; then
  fail_update "Wavo Swagger refresh failed to create a same-directory temporary file."
fi
if ! cp "$physical_source_contract" "$temporary_file"; then
  fail_update "Wavo Swagger refresh failed to copy the source contract."
fi

# Re-hash and revalidate the copy so a partial or concurrently changed source
# can never be renamed over the bundled snapshot.
set +e
temporary_object=$(safe_git -C "$source_root" hash-object -- "$temporary_file")
temporary_hash_status=$?
set -e
if [ "$temporary_hash_status" -ne 0 ] || [ "$temporary_object" != "$index_object" ]; then
  fail_update "Copied Wavo Swagger bytes do not match the reviewed index blob."
fi
validate_contract "$temporary_file"
if ! chmod 0644 "$temporary_file"; then
  fail_update "Wavo Swagger refresh failed to set the temporary file mode."
fi

set +e
temporary_source_file=$(mktemp "$destination_dir/.WAVO_SWAGGER_SOURCE.tmp.XXXXXX")
temporary_source_status=$?
set -e
if [ "$temporary_source_status" -ne 0 ] || [ -z "$temporary_source_file" ]; then
  fail_update "Wavo Swagger refresh failed to create a provenance temporary file."
fi
if ! printf 'mywavo %s\n' "$source_sha" >"$temporary_source_file" || \
  ! chmod 0644 "$temporary_source_file"; then
  fail_update "Wavo Swagger refresh failed to stage source provenance."
fi

# Keep same-directory rollback copies until both destinations have been
# replaced. Any ordinary failure restores the original pair.
contract_backup=$(mktemp "$destination_dir/.application_swagger.json.backup.XXXXXX") || \
  fail_update "Wavo Swagger refresh failed to stage contract rollback data."
source_backup=$(mktemp "$destination_dir/.WAVO_SWAGGER_SOURCE.backup.XXXXXX") || \
  fail_update "Wavo Swagger refresh failed to stage provenance rollback data."
if ! cp "$destination" "$contract_backup" || ! cp "$source_destination" "$source_backup"; then
  fail_update "Wavo Swagger refresh failed to preserve existing destinations."
fi

replacement_started=1
if ! mv "$temporary_file" "$destination"; then
  fail_update "Wavo Swagger refresh failed to replace the bundled contract."
fi
temporary_file=''
if ! mv "$temporary_source_file" "$source_destination"; then
  if ! cp "$contract_backup" "$destination" || ! cp "$source_backup" "$source_destination"; then
    printf '%s\n' "Wavo Swagger refresh failed and could not restore both destinations." >&2
    exit 2
  fi
  replacement_started=0
  fail_update "Wavo Swagger refresh failed to replace source provenance."
fi
temporary_source_file=''
replacement_started=0
rm -f "$contract_backup" "$source_backup" || fail_update "Wavo Swagger refresh could not remove rollback files."
contract_backup=''
source_backup=''
printf '%s\n' "Updated the bundled Wavo Swagger contract and provenance from the validated local source checkout."
