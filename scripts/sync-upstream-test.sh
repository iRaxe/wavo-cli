#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd -P)
script="$repo_root/scripts/sync-upstream.sh"
test_root=$(mktemp -d /tmp/wavo-sync-test.XXXXXX)
upstream_owner='chat'"woot"

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  case "$test_root" in
    /tmp/wavo-sync-test.*) rm -rf "$test_root" ;;
    *) printf '%s\n' 'not ok - refusing unsafe harness cleanup' >&2; exit 1 ;;
  esac
  exit "$status"
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM

fail() { printf 'not ok - %s\n' "$1" >&2; exit 1; }
pass() { printf 'ok - %s\n' "$1"; }

git_quiet() { git "$@" >/dev/null 2>&1; }

new_fixture() {
  name=$1
  fixture="$test_root/$name"
  seed="$fixture/seed"
  origin="$fixture/origin.git"
  upstream="$fixture/upstream.git"
  work="$fixture/work"
  mkdir -p "$fixture"
  git_quiet init --bare --initial-branch=main "$origin"
  git_quiet init --bare --initial-branch=main "$upstream"
  git_quiet init --initial-branch=main "$seed"
  git -C "$seed" config user.name 'Wavo Sync Test'
  git -C "$seed" config user.email 'sync@example.invalid'
  printf '%s\n' base >"$seed/shared.txt"
  mkdir -p "$seed/scripts"
  cp "$repo_root/scripts/safe-git.sh" "$seed/scripts/safe-git.sh"
  cp "$repo_root/scripts/sync-git-test-double.sh" "$seed/scripts/sync-git.sh"
  cp "$script" "$seed/scripts/sync-upstream.sh"
  chmod 0755 "$seed/scripts/sync-upstream.sh"
  git_quiet -C "$seed" add .
  git_quiet -C "$seed" commit -m base
  git_quiet -C "$seed" remote add origin "$origin"
  git_quiet -C "$seed" push origin main
  git_quiet -C "$seed" remote add upstream "$upstream"
  git_quiet -C "$seed" push upstream main
  git_quiet clone "$origin" "$work"
  git -C "$work" config user.name 'Wavo Sync Test'
  git -C "$work" config user.email 'sync@example.invalid'
  git_quiet -C "$work" remote set-url origin https://github.com/iRaxe/wavo-cli.git
  git_quiet -C "$work" remote add upstream "https://github.com/$upstream_owner/cli.git"
  git_quiet -C "$work" remote set-url --push upstream no_push
  git -C "$work" config wavo.syncTestOrigin "$origin"
  git -C "$work" config wavo.syncTestUpstream "$upstream"
}

run_sync() {
  (cd "$work" && ./scripts/sync-upstream.sh) >"$fixture/stdout" 2>"$fixture/stderr"
}

expect_failure() {
  expected=$1
  if run_sync; then
    fail "$expected"
  fi
  grep -Fq "$expected" "$fixture/stderr" || fail "missing diagnostic: $expected"
}

if [ ! -x "$script" ]; then
  fail 'sync script exists and is executable'
fi

new_fixture wrong-branch
git_quiet -C "$work" switch -c topic
git -C "$work" remote set-url origin "$fixture/missing-origin.git"
expect_failure 'Run upstream sync from main.'
pass 'wrong branch fails before any fetch'

new_fixture dirty
printf '%s\n' dirty >>"$work/shared.txt"
git -C "$work" remote set-url origin "$fixture/missing-origin.git"
expect_failure 'Working tree must be clean.'
pass 'dirty worktree fails before any fetch'

new_fixture missing-remote
git_quiet -C "$work" remote remove upstream
expect_failure "Required remote 'upstream' is missing."
test "$(git -C "$work" branch --show-current)" = main || fail 'missing remote changed branch'
pass 'required remotes are validated without changing branches'

new_fixture multiple-fetch
git -C "$work" config --add remote.origin.url https://github.com/iRaxe/wavo-cli.git
expect_failure "Remote 'origin' must have exactly one fetch URL."
test "$(git -C "$work" branch --show-current)" = main || fail 'multiple fetch URLs changed branch'
pass 'multiple fetch URLs are rejected before network or branch mutation'

spoof_index=0
for rejected_url in \
  'https://user@github.com/iRaxe/wavo-cli.git' \
  'https://github.com:443/iRaxe/wavo-cli.git' \
  'https://github.com/iRaxe/wavo-cli.git?ref=main' \
  'https://github.com/iRaxe/wavo-cli.git#main' \
  'https://GitHub.com/iRaxe/wavo-cli.git' \
  'https://github.com.evil.invalid/iRaxe/wavo-cli.git' \
  'https://github.com/iRaxe/wavo-cli/extra'; do
  spoof_index=$((spoof_index + 1))
  new_fixture "spoofed-origin-$spoof_index"
  printf '%s\n' "$spoof_index" >"$seed/spoof-probe.txt"
  git_quiet -C "$seed" add spoof-probe.txt
  git_quiet -C "$seed" commit -m spoof-probe
  git_quiet -C "$seed" push origin main
  before_tracking=$(git -C "$work" rev-parse refs/remotes/origin/main)
  git -C "$work" config remote.origin.url "$rejected_url"
  expect_failure "Remote 'origin' fetch URL is not the canonical Wavo CLI repository."
  test "$(git -C "$work" branch --show-current)" = main || fail 'spoofed origin changed branch'
  test "$(git -C "$work" rev-parse refs/remotes/origin/main)" = "$before_tracking" || fail 'spoofed origin was fetched'
done
pass 'credentials, ports, query, fragment, case, host, and path spoofs fail before fetch'

new_fixture swapped-remotes
git -C "$work" config remote.origin.url "https://github.com/$upstream_owner/cli.git"
git -C "$work" config remote.upstream.url https://github.com/iRaxe/wavo-cli.git
expect_failure "Remote 'origin' fetch URL is not the canonical Wavo CLI repository."
test "$(git -C "$work" branch --show-current)" = main || fail 'swapped remotes changed branch'
pass 'swapped canonical repositories fail before fetch'

new_fixture rewrite-attack
before_tracking=$(git -C "$work" rev-parse refs/remotes/origin/main)
git_quiet init --bare --initial-branch=main "$fixture/attacker.git"
git -C "$work" config "url.$fixture/attacker.git.insteadOf" https://github.com/
expect_failure 'Git URL rewrite configuration is not allowed for upstream sync.'
test "$(git -C "$work" rev-parse refs/remotes/origin/main)" = "$before_tracking" || fail 'rewrite attack changed tracking refs'
test "$(git -C "$work" branch --show-current)" = main || fail 'rewrite attack changed branch'
pass 'canonical literal remotes with an attacker rewrite fail before network or mutation'

new_fixture mixed-case-rewrite
git -C "$work" config "url.$fixture/attacker.git.pushInsteadOf" https://github.com/
expect_failure 'Git URL rewrite configuration is not allowed for upstream sync.'
test "$(git -C "$work" branch --show-current)" = main || fail 'mixed-case rewrite changed branch'
pass 'pushInsteadOf keys are rejected case-insensitively'

new_fixture ssh-origin
git -C "$work" config remote.origin.url git@github.com:iRaxe/wavo-cli.git
expect_failure "Remote 'origin' fetch URL is not the canonical Wavo CLI repository."
pass 'SSH fetch URLs are rejected'

new_fixture wrong-default
git_quiet -C "$seed" switch -c develop
git_quiet -C "$seed" push upstream develop
git --git-dir="$upstream" symbolic-ref HEAD refs/heads/develop
expect_failure "Remote 'upstream' default branch must be main."
test "$(git -C "$work" branch --show-current)" = main || fail 'wrong default changed branch'
pass 'remote default branches are validated'

new_fixture push-enabled
git_quiet -C "$work" remote set-url --push upstream "$upstream"
expect_failure "Remote 'upstream' push URL must remain disabled as no_push."
test "$(git -C "$work" branch --show-current)" = main || fail 'push guard changed branch'
pass 'upstream push access is rejected before fetching'

new_fixture multiple-push
git -C "$work" config --add remote.upstream.pushurl https://github.com/iRaxe/wavo-cli.git
expect_failure "Remote 'upstream' must have exactly one disabled push URL."
test "$(git -C "$work" branch --show-current)" = main || fail 'multiple push URLs changed branch'
pass 'no_push plus a second upstream push URL is rejected'

new_fixture missing-ref
git --git-dir="$upstream" update-ref -d refs/heads/main
git --git-dir="$upstream" symbolic-ref HEAD refs/heads/main
expect_failure "Remote 'upstream' does not advertise main."
test "$(git -C "$work" branch --show-current)" = main || fail 'missing ref changed branch'
pass 'missing remote main refs are rejected'

new_fixture equal
git_quiet -C "$seed" tag equal-upstream-only
git_quiet -C "$seed" push upstream refs/tags/equal-upstream-only
before_origin=$(git --git-dir="$origin" rev-parse refs/heads/main)
before_upstream=$(git --git-dir="$upstream" rev-parse refs/heads/main)
before_tags=$(git -C "$work" tag)
run_sync || fail 'equal histories were not handled cleanly'
grep -Fq 'No upstream changes to stage.' "$fixture/stdout" || fail 'equal histories lacked explicit result'
test "$(git -C "$work" branch --show-current)" = main || fail 'equal histories stranded a branch'
test -z "$(git -C "$work" status --porcelain)" || fail 'equal histories dirtied the worktree'
if git -C "$work" show-ref --verify --quiet "refs/heads/sync/upstream-$(date +%Y%m%d)"; then fail 'equal histories left a sync branch'; fi
test "$(git --git-dir="$origin" rev-parse refs/heads/main)" = "$before_origin" || fail 'equal histories changed origin'
test "$(git --git-dir="$upstream" rev-parse refs/heads/main)" = "$before_upstream" || fail 'equal histories changed upstream'
test "$(git -C "$work" tag)" = "$before_tags" || fail 'equal histories fetched tags'
pass 'equal histories return cleanly on main without a sync branch'

new_fixture ancestor
printf '%s\n' origin-new >"$seed/origin-new.txt"
git_quiet -C "$seed" add origin-new.txt
git_quiet -C "$seed" commit -m origin-new
git_quiet -C "$seed" push origin main
run_sync || fail 'ancestor upstream was not handled cleanly'
grep -Fq 'No upstream changes to stage.' "$fixture/stdout" || fail 'ancestor case lacked explicit result'
test "$(git -C "$work" branch --show-current)" = main || fail 'ancestor case stranded a branch'
test -z "$(git -C "$work" status --porcelain)" || fail 'ancestor case dirtied the worktree'
if git -C "$work" show-ref --verify --quiet "refs/heads/sync/upstream-$(date +%Y%m%d)"; then fail 'ancestor case left a sync branch'; fi
pass 'already-merged upstream returns cleanly without mutation'

new_fixture clean-merge
printf '%s\n' origin >"$seed/origin.txt"
git_quiet -C "$seed" add origin.txt
git_quiet -C "$seed" commit -m origin
git_quiet -C "$seed" push origin main
git_quiet -C "$seed" reset --hard HEAD~1
printf '%s\n' upstream >"$seed/upstream.txt"
git_quiet -C "$seed" add upstream.txt
git_quiet -C "$seed" commit -m upstream
git_quiet -C "$seed" push upstream main
git_quiet -C "$seed" tag upstream-only
git_quiet -C "$seed" push upstream refs/tags/upstream-only
before_origin=$(git --git-dir="$origin" rev-parse refs/heads/main)
before_upstream=$(git --git-dir="$upstream" rev-parse refs/heads/main)
before_tags=$(git -C "$work" tag)
run_sync || fail 'clean upstream preparation failed'
branch="sync/upstream-$(date +%Y%m%d)"
test "$(git -C "$work" branch --show-current)" = "$branch" || fail 'sync branch was not selected'
git -C "$work" rev-parse -q --verify MERGE_HEAD >/dev/null || fail 'merge was not left staged'
test "$(git -C "$work" log -1 --format=%H)" = "$before_origin" || fail 'script created a commit'
test "$(git --git-dir="$origin" rev-parse refs/heads/main)" = "$before_origin" || fail 'script changed origin'
test "$(git --git-dir="$upstream" rev-parse refs/heads/main)" = "$before_upstream" || fail 'script changed upstream'
test "$(git -C "$work" tag)" = "$before_tags" || fail 'script fetched or created tags'
pass 'clean merge is staged without commit, push, tag, or publish'

git_quiet -C "$work" merge --abort
git_quiet -C "$work" switch main
expect_failure "Sync branch '$branch' already exists."
test "$(git -C "$work" branch --show-current)" = main || fail 'collision changed branch'
pass 'repeat run rejects the deterministic branch collision'

new_fixture conflict
printf '%s\n' origin >"$seed/shared.txt"
git_quiet -C "$seed" add shared.txt
git_quiet -C "$seed" commit -m origin-conflict
git_quiet -C "$seed" push origin main
git_quiet -C "$seed" reset --hard HEAD~1
printf '%s\n' upstream >"$seed/shared.txt"
git_quiet -C "$seed" add shared.txt
git_quiet -C "$seed" commit -m upstream-conflict
git_quiet -C "$seed" push upstream main
expect_failure 'Resolve conflicts, then run all Wavo gates.'
branch="sync/upstream-$(date +%Y%m%d)"
test "$(git -C "$work" branch --show-current)" = "$branch" || fail 'conflict did not remain on sync branch'
git -C "$work" rev-parse -q --verify MERGE_HEAD >/dev/null || fail 'conflict merge state was discarded'
test -n "$(git -C "$work" diff --name-only --diff-filter=U)" || fail 'conflict was not preserved for review'
pass 'merge conflicts remain inspectable and unpublished'
