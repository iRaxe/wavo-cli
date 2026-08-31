#!/usr/bin/env sh
set -eu

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd -P)
# shellcheck source=scripts/safe-git.sh
. "$script_dir/safe-git.sh"
# shellcheck source=scripts/sync-git.sh
. "$script_dir/sync-git.sh"
repo_root=$(safe_git -C "$script_dir/.." rev-parse --show-toplevel)

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

current_branch=$(safe_git -C "$repo_root" branch --show-current)
test "$current_branch" = main || fail 'Run upstream sync from main.'
test -z "$(safe_git -C "$repo_root" status --porcelain)" || fail 'Working tree must be clean.'

canonical_fetch_url() {
  remote=$1
  repository=$2
  urls=$(sync_network_git -C "$repo_root" config --get-all "remote.$remote.url") || \
    fail "Required remote '$remote' is missing."
  url_count=$(printf '%s\n' "$urls" | wc -l | tr -d ' ')
  test "$url_count" -eq 1 || fail "Remote '$remote' must have exactly one fetch URL."
  case "$urls" in
    "https://github.com/$repository.git") ;;
    *)
      if test "$remote" = origin; then
        fail "Remote 'origin' fetch URL is not the canonical Wavo CLI repository."
      fi
      fail "Remote 'upstream' fetch URL is not the canonical upstream CLI repository."
      ;;
  esac
}

upstream_owner='chat'"woot"
canonical_fetch_url origin iRaxe/wavo-cli
canonical_fetch_url upstream "$upstream_owner/cli"

set +e
rewrite_config=$(sync_network_git -C "$repo_root" config --includes --show-origin --list)
rewrite_status=$?
set -e
test "$rewrite_status" -eq 0 || fail 'Could not inspect effective Git URL configuration.'
if printf '%s\n' "$rewrite_config" | grep -i -E 'url\..*\.(insteadof|pushinsteadof)=' >/dev/null; then
  fail 'Git URL rewrite configuration is not allowed for upstream sync.'
fi

upstream_push_urls=$(safe_git -C "$repo_root" remote get-url --push --all upstream)
push_url_count=$(printf '%s\n' "$upstream_push_urls" | wc -l | tr -d ' ')
test "$push_url_count" -eq 1 || \
  fail "Remote 'upstream' must have exactly one disabled push URL."
test "$upstream_push_urls" = no_push || \
  fail "Remote 'upstream' push URL must remain disabled as no_push."

branch="sync/upstream-$(date +%Y%m%d)"
if safe_git -C "$repo_root" show-ref --verify --quiet "refs/heads/$branch"; then
  fail "Sync branch '$branch' already exists."
fi

validate_remote_main() {
  remote=$1
  url=$2
  set +e
  advertised=$(sync_network_git -C "$repo_root" ls-remote --symref "$url" HEAD refs/heads/main)
  status=$?
  set -e
  test "$status" -eq 0 || fail "Could not inspect remote '$remote'."
  printf '%s\n' "$advertised" | grep -Eq '[[:space:]]refs/heads/main$' || \
    fail "Remote '$remote' does not advertise main."
  printf '%s\n' "$advertised" | grep -Fq "ref: refs/heads/main" || \
    fail "Remote '$remote' default branch must be main."
}

origin_url=https://github.com/iRaxe/wavo-cli.git
upstream_url="https://github.com/$upstream_owner/cli.git"
validate_remote_main origin "$origin_url"
validate_remote_main upstream "$upstream_url"

sync_network_git -C "$repo_root" fetch --no-tags "$origin_url" \
  '+refs/heads/main:refs/remotes/origin/main'
sync_network_git -C "$repo_root" fetch --no-tags "$upstream_url" \
  '+refs/heads/main:refs/remotes/upstream/main'

safe_git -C "$repo_root" cat-file -e 'refs/remotes/origin/main^{commit}' || \
  fail "Fetched origin/main is not a commit."
safe_git -C "$repo_root" cat-file -e 'refs/remotes/upstream/main^{commit}' || \
  fail "Fetched upstream/main is not a commit."

if safe_git -C "$repo_root" merge-base --is-ancestor upstream/main origin/main; then
  printf '%s\n' 'No upstream changes to stage.'
  exit 0
fi

safe_git -C "$repo_root" -c core.hooksPath=/dev/null switch -c "$branch" origin/main
if ! safe_git -C "$repo_root" -c core.hooksPath=/dev/null merge --no-commit --no-ff upstream/main; then
  fail 'Resolve conflicts, then run all Wavo gates.'
fi

cleanup_empty_merge() {
  safe_git -C "$repo_root" -c core.hooksPath=/dev/null merge --abort >/dev/null 2>&1 || true
  safe_git -C "$repo_root" -c core.hooksPath=/dev/null switch main >/dev/null
  safe_git -C "$repo_root" -c core.hooksPath=/dev/null branch -D "$branch" >/dev/null
}

if ! safe_git -C "$repo_root" rev-parse -q --verify MERGE_HEAD >/dev/null; then
  cleanup_empty_merge
  fail 'Upstream preparation did not leave a reviewable merge state.'
fi
set +e
safe_git -C "$repo_root" diff --cached --quiet
staged_status=$?
set -e
case "$staged_status" in
  1) ;;
  0)
    cleanup_empty_merge
    fail 'Upstream preparation did not stage any file changes.'
    ;;
  *)
    cleanup_empty_merge
    fail 'Upstream preparation could not verify staged changes.'
    ;;
esac

printf '%s\n' 'Merge staged. Run unit, race, dev-profile, contract, branding, shellcheck, and release gates.'
