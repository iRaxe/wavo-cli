#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd -P)
set -- "$repo_root"/.github/workflows/*.yml "$repo_root"/.github/workflows/*.yaml

fail=0
for workflow in "$@"; do
  test -f "$workflow" || continue
  if ! awk '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    {
      content=$0
      sub(/[[:space:]]+#.*$/, "", content)
      plain=trim(content)
      sub(/^-[[:space:]]+/, "", plain)
      if (plain ~ /^uses:[[:space:]]*/) {
        sub(/^uses:[[:space:]]*/, "", plain)
        if (plain ~ /@/ && plain !~ /@[0-9a-f]{40}$/) bad=1
      }
      if (plain ~ /^version:[[:space:]]*["'"'"']?latest["'"'"']?$/) bad=1
      if (content ~ /go install [^#[:space:]]+@latest([[:space:]]|$)/) bad=1
    }
    END { exit bad ? 1 : 0 }
  ' "$workflow"; then
    printf '%s\n' "Floating action or executable tool reference in $workflow." >&2
    fail=1
  fi
done

test "$fail" -eq 0
