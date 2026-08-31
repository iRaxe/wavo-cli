#!/usr/bin/env sh

# Offline transport used only by sync-upstream-test.sh copies. Production uses
# sync-git.sh and has no configurable transport bypass.
# shellcheck disable=SC2154 # assigned by the fixture's safe-git.sh source
sync_network_git() {
  test "$1" = -C
  sync_test_repo=$2
  shift 2
  case "$1" in
    config)
      "$safe_git_executable" -C "$sync_test_repo" "$@"
      ;;
    ls-remote)
      shift
      test "$1" = --symref
      shift
      sync_test_url=$1
      shift
      sync_test_target=$(sync_test_remote_target "$sync_test_repo" "$sync_test_url")
      "$safe_git_executable" -C "$sync_test_repo" ls-remote --symref "$sync_test_target" "$@"
      ;;
    fetch)
      shift
      test "$1" = --no-tags
      shift
      sync_test_url=$1
      shift
      sync_test_target=$(sync_test_remote_target "$sync_test_repo" "$sync_test_url")
      "$safe_git_executable" -C "$sync_test_repo" fetch --no-tags "$sync_test_target" "$@"
      ;;
    *) return 2 ;;
  esac
}

sync_test_remote_target() {
  sync_test_repo=$1
  sync_test_url=$2
  upstream_owner='chat'"woot"
  case "$sync_test_url" in
    https://github.com/iRaxe/wavo-cli.git)
      "$safe_git_executable" -C "$sync_test_repo" config --get wavo.syncTestOrigin
      ;;
    "https://github.com/$upstream_owner/cli.git")
      "$safe_git_executable" -C "$sync_test_repo" config --get wavo.syncTestUpstream
      ;;
    *) return 2 ;;
  esac
}
