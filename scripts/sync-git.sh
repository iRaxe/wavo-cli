#!/usr/bin/env sh

# Run network-capable Git operations without caller, global, or system Git
# configuration. Repository-local configuration remains visible so the sync
# script can reject URL rewrites before making a network request.
# shellcheck disable=SC2154 # assigned by the required safe-git.sh source
sync_network_git() {
  env -i \
    PATH="$safe_system_path" \
    LC_ALL=C \
    HOME=/dev/null \
    GIT_CONFIG_NOSYSTEM=1 \
    GIT_CONFIG_GLOBAL=/dev/null \
    "$safe_git_executable" "$@"
}
