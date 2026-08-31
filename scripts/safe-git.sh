#!/bin/sh

# This file is sourced by repository maintenance scripts. Reset PATH before
# resolving or invoking any security-boundary utility; caller PATH entries are
# never appended or consulted.
safe_system_path=/usr/bin:/bin
PATH=$safe_system_path
export PATH

safe_git_executable=''
for safe_git_candidate in /usr/bin/git /bin/git; do
  if [ -f "$safe_git_candidate" ] && [ -x "$safe_git_candidate" ]; then
    safe_git_executable=$safe_git_candidate
    break
  fi
done
if [ -z "$safe_git_executable" ]; then
  set +e
  safe_git_executable=$(PATH=$safe_system_path command -p -v git)
  safe_git_lookup_status=$?
  set -e
  if [ "$safe_git_lookup_status" -ne 0 ] || [ -z "$safe_git_executable" ]; then
    printf '%s\n' "Wavo maintenance requires Git in a trusted system location." >&2
    exit 2
  fi
fi
case "$safe_git_executable" in
  /*) ;;
  *)
    printf '%s\n' "Wavo maintenance could not resolve an absolute trusted Git executable path." >&2
    exit 2
    ;;
esac
if [ ! -f "$safe_git_executable" ] || [ ! -x "$safe_git_executable" ]; then
  printf '%s\n' "Wavo maintenance rejected a non-executable trusted Git candidate." >&2
  exit 2
fi

safe_go_executable=''
for safe_go_candidate in \
  /usr/local/go/bin/go \
  /usr/bin/go \
  /opt/homebrew/bin/go \
  /usr/local/bin/go; do
  if [ -f "$safe_go_candidate" ] && [ -x "$safe_go_candidate" ]; then
    safe_go_executable=$safe_go_candidate
    break
  fi
done
if [ -z "$safe_go_executable" ]; then
  printf '%s\n' "Wavo maintenance requires Go in a trusted standard system location." >&2
  exit 2
fi

safe_git() {
  env -i PATH="$safe_system_path" LC_ALL=C "$safe_git_executable" "$@"
}

# Go may inspect VCS metadata or invoke Git while resolving modules. Every call
# receives a freshly created private context root from its caller; never query
# `go env` in the caller environment or reuse caller home/cache settings. A
# standard system Go installation is required.
safe_go_in_context() {
  safe_go_toolchain=$1
  shift
  if [ "$#" -lt 2 ]; then
    printf '%s\n' "Wavo maintenance requires a private Go validation context." >&2
    return 2
  fi
  safe_go_context=$1
  shift
  case "$safe_go_context" in
    /*) ;;
    *)
      printf '%s\n' "Wavo maintenance rejected a non-absolute Go validation context." >&2
      return 2
      ;;
  esac
  if [ ! -d "$safe_go_context" ]; then
    printf '%s\n' "Wavo maintenance rejected a missing Go validation context." >&2
    return 2
  fi
  safe_go_home="$safe_go_context/home"
  safe_go_path="$safe_go_context/gopath"
  safe_go_modcache="$safe_go_path/pkg/mod"
  safe_go_cache="$safe_go_context/build-cache"
  if ! mkdir -p "$safe_go_home" "$safe_go_modcache" "$safe_go_cache"; then
    printf '%s\n' "Wavo maintenance could not initialize the private Go validation context." >&2
    return 2
  fi
  if ! chmod 0700 "$safe_go_context" "$safe_go_home" "$safe_go_path" "$safe_go_modcache" "$safe_go_cache"; then
    printf '%s\n' "Wavo maintenance could not secure the private Go validation context." >&2
    return 2
  fi
  if ! env -i \
    PATH="$safe_system_path" \
    LC_ALL=C \
    HOME="$safe_go_home" \
    GOPATH="$safe_go_path" \
    GOMODCACHE="$safe_go_modcache" \
    GOCACHE="$safe_go_cache" \
    GOENV=off \
    GOFLAGS='-buildvcs=false -mod=readonly' \
    GOWORK=off \
    GOTOOLCHAIN="$safe_go_toolchain" \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org \
    "$safe_go_executable" telemetry off >/dev/null 2>&1; then
    printf '%s\n' "Wavo maintenance could not disable telemetry in the private Go validation context." >&2
    return 2
  fi
  env -i \
    PATH="$safe_system_path" \
    LC_ALL=C \
    HOME="$safe_go_home" \
    GOPATH="$safe_go_path" \
    GOMODCACHE="$safe_go_modcache" \
    GOCACHE="$safe_go_cache" \
    GOENV=off \
    GOFLAGS='-buildvcs=false -mod=readonly' \
    GOWORK=off \
    GOTOOLCHAIN="$safe_go_toolchain" \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org \
    "$safe_go_executable" "$@"
}

# The repository validator needs automatic acquisition because go.mod pins a
# newer patch toolchain than some standard system installations provide.
safe_go() {
  safe_go_in_context auto "$@"
}

# Dependency-free temporary helper modules use only the trusted local Go
# installation and must not acquire an unnecessary toolchain.
safe_go_local() {
  safe_go_in_context local "$@"
}
