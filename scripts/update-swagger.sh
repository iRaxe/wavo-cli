#!/bin/sh
set -eu
PATH=/usr/bin:/bin
export PATH

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd -P)
repo_root=$(CDPATH='' cd "$script_dir/.." && pwd -P)
temporary_file=''
validation_root=''
validator_binary=''

# shellcheck source=scripts/safe-git.sh
. "$script_dir/safe-git.sh"

cleanup() {
  if [ -n "$temporary_file" ] && [ -e "$temporary_file" ]; then
    rm -f "$temporary_file"
  fi
  if [ -n "$validation_root" ]; then
    case "$validation_root" in
      /tmp/wavo-swagger-validation.*)
        if [ -d "$validation_root" ]; then
          rm -rf "$validation_root"
        fi
        ;;
    esac
  fi
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
if [ ! -d "$destination_dir" ]; then
  fail_update "Wavo Swagger destination directory does not exist: $destination_dir"
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
if ! mv "$temporary_file" "$destination"; then
  fail_update "Wavo Swagger refresh failed to atomically replace the bundled contract."
fi
temporary_file=''
printf '%s\n' "Updated the bundled Wavo Swagger contract from the validated local source checkout."
