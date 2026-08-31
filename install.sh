#!/usr/bin/env sh
# install.sh — download a wavo-cli release binary into a local bin dir.
#
# Usage:
#   curl -fsSL https://my.wavo.it/install-cli | sh
#
# Environment:
#   WAVO_VERSION      pin a specific version, e.g. "v0.2.1" (default: latest)
#   WAVO_INSTALL_DIR  install location (default: $HOME/.local/bin)
#   NO_COLOR          set to disable color/banner output

set -eu

REPO="iRaxe/wavo-cli"
BINARY="wavo"
INSTALL_DIR="${WAVO_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${WAVO_VERSION:-latest}"
requested_latest=false
[ "$VERSION" = "latest" ] && requested_latest=true

valid_release_tag() {
  printf '%s\n' "$1" | LC_ALL=C awk '
    NR != 1 { valid=0 }
    NR == 1 {
      valid=1
      if ($0 !~ /^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$/) valid=0
      value=substr($0, 2)
      plus=index(value, "+")
      core_and_pre=value
      if (plus) {
        core_and_pre=substr(value, 1, plus-1)
        if (!identifiers(substr(value, plus+1), 0)) valid=0
      }
      dash=index(core_and_pre, "-")
      core_value=core_and_pre
      if (dash) {
        core_value=substr(core_and_pre, 1, dash-1)
        if (!identifiers(substr(core_and_pre, dash+1), 1)) valid=0
      }
      core_count=split(core_value, core, ".")
      if (core_count != 3) valid=0
      for (i=1; i<=3; i++) {
        if (core[i] !~ /^(0|[1-9][0-9]*)$/) valid=0
      }
    }
    function identifiers(value, reject_leading_zero, parts, count, i) {
      count=split(value, parts, ".")
      for (i=1; i<=count; i++) {
        if (parts[i] == "" || parts[i] !~ /^[0-9A-Za-z-]+$/) return 0
        if (reject_leading_zero && parts[i] ~ /^[0-9]+$/ && parts[i] ~ /^0[0-9]+$/) return 0
      }
      return 1
    }
    END { exit valid ? 0 : 1 }
  '
}

release_is_newer_or_equal() {
  printf '%s\n%s\n' "$1" "$2" | LC_ALL=C awk '
    NR == 1 { candidate=$0 }
    NR == 2 { installed=$0 }
    END {
      candidate_pre_count=split_version(candidate, candidate_core, candidate_pre)
      installed_pre_count=split_version(installed, installed_core, installed_pre)
      for (i=1; i<=3; i++) {
        comparison=compare_numeric(candidate_core[i], installed_core[i])
        if (comparison != 0) exit comparison > 0 ? 0 : 1
      }
      candidate_has_pre=(candidate_pre[1] != "")
      installed_has_pre=(installed_pre[1] != "")
      if (!candidate_has_pre && installed_has_pre) exit 0
      if (candidate_has_pre && !installed_has_pre) exit 1
      count=(candidate_pre_count > installed_pre_count ? candidate_pre_count : installed_pre_count)
      for (i=1; i<=count; i++) {
        if (!(i in candidate_pre)) exit 1
        if (!(i in installed_pre)) exit 0
        candidate_numeric=(candidate_pre[i] ~ /^[0-9]+$/)
        installed_numeric=(installed_pre[i] ~ /^[0-9]+$/)
        if (candidate_numeric && installed_numeric) comparison=compare_numeric(candidate_pre[i], installed_pre[i])
        else if (candidate_numeric) comparison=-1
        else if (installed_numeric) comparison=1
        else comparison=(candidate_pre[i] == installed_pre[i] ? 0 : (candidate_pre[i] > installed_pre[i] ? 1 : -1))
        if (comparison != 0) exit comparison > 0 ? 0 : 1
      }
      exit 0
    }
    function split_version(value, core, prerelease, without_build, dash, pre, pre_count) {
      value=substr(value, 2)
      sub(/\+.*/, "", value)
      dash=index(value, "-")
      without_build=value
      if (dash) {
        without_build=substr(value, 1, dash-1)
        pre=substr(value, dash+1)
        pre_count=split(pre, prerelease, ".")
      }
      split(without_build, core, ".")
      return pre_count
    }
    function compare_numeric(left, right) {
      if (length(left) != length(right)) return length(left) > length(right) ? 1 : -1
      return left == right ? 0 : (left > right ? 1 : -1)
    }
  '
}

# ---------------------------------------------------------------------------
# Pretty output
# ---------------------------------------------------------------------------
# Colors only when stdout is a TTY and NO_COLOR isn't set.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_RESET=$(printf '\033[0m')
  C_DIM=$(printf '\033[2m')
  C_BOLD=$(printf '\033[1m')
  C_CYAN=$(printf '\033[36m')
  C_GREEN=$(printf '\033[32m')
  C_YELLOW=$(printf '\033[33m')
  C_RED=$(printf '\033[31m')
else
  C_RESET=''; C_DIM=''; C_BOLD=''; C_CYAN=''; C_GREEN=''; C_YELLOW=''; C_RED=''
fi

step() { printf '  %s→%s %s\n' "$C_CYAN" "$C_RESET" "$*"; }
ok()   { printf '  %s✓%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
warn() { printf '  %s!%s %s\n' "$C_YELLOW" "$C_RESET" "$*"; }
err()  { printf '  %s✗%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 1; }
has()  { command -v "$1" >/dev/null 2>&1; }

banner() {
  [ -t 1 ] || return 0
  [ -z "${NO_COLOR:-}" ] || return 0
  printf '\n%s' "$C_CYAN"
  cat <<'EOF'
  W A V O   C L I
EOF
  printf '%s\n' "$C_RESET"
}

banner

# ---------------------------------------------------------------------------
# Detect platform
# ---------------------------------------------------------------------------
case "$(uname -s)" in
  Darwin) os=Darwin ;;
  Linux)  os=Linux ;;
  *) err "unsupported OS: $(uname -s) (Windows users: download from https://github.com/$REPO/releases)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=x86_64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported arch: $(uname -m)" ;;
esac

step "Detected ${os}/${arch}"

# ---------------------------------------------------------------------------
# Detect existing install
# ---------------------------------------------------------------------------
existing_path="$INSTALL_DIR/$BINARY"
version_path="$INSTALL_DIR/.wavo-version"
existing=false
installed_version=""
if [ -e "$existing_path" ] || [ -L "$existing_path" ]; then
  if [ ! -f "$existing_path" ] || [ -L "$existing_path" ]; then
    err "refusing to replace a non-regular or symbolic-link target: $existing_path"
  fi
  existing=true
  step "Found existing ${BINARY}"
fi
if [ -e "$version_path" ] || [ -L "$version_path" ]; then
  if [ ! -f "$version_path" ] || [ -L "$version_path" ]; then
    err "refusing to replace an unsafe version record: $version_path"
  fi
  installed_version=$(awk 'NR == 1 { value=$0 } NR > 1 { invalid=1 } END { if (!invalid) print value }' "$version_path")
  valid_release_tag "$installed_version" || installed_version=""
fi

# ---------------------------------------------------------------------------
# Resolve version (latest by default)
# ---------------------------------------------------------------------------
if [ "$VERSION" = "latest" ]; then
  # The releases collection includes prereleases; GitHub orders it newest first.
  VERSION=$(curl -fsSL --connect-timeout 10 --max-time 30 --max-filesize 1048576 \
    -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/$REPO/releases?per_page=1" 2>/dev/null \
    | awk -F'"' '/"tag_name"[[:space:]]*:/{print $4; exit}')
  [ -n "$VERSION" ] || err "could not resolve latest Wavo CLI version"
fi
valid_release_tag "$VERSION" || err "invalid Wavo CLI release tag"
if [ "$requested_latest" = true ] && [ "$existing" = true ]; then
  [ -n "$installed_version" ] || err "cannot safely compare the installed version; use an explicit WAVO_VERSION"
  release_is_newer_or_equal "$VERSION" "$installed_version" || err "refusing an implicit downgrade from $installed_version to $VERSION"
fi
ver_clean="${VERSION#v}"

if [ "$existing" = false ]; then
  install_mode=install
  step "Resolved version ${C_BOLD}${VERSION}${C_RESET}"
elif [ "$installed_version" = "$VERSION" ]; then
  install_mode=reinstall
  step "Resolved version ${C_BOLD}${VERSION}${C_RESET} (already up to date — reinstalling)"
else
  install_mode=update
  step "Installing ${C_BOLD}${VERSION}${C_RESET} over the existing binary"
fi

# ---------------------------------------------------------------------------
# Build URLs and download
# ---------------------------------------------------------------------------
asset="${BINARY}_${ver_clean}_${os}_${arch}.tar.gz"
asset_url="https://github.com/$REPO/releases/download/${VERSION}/${asset}"
checksum_url="https://github.com/$REPO/releases/download/${VERSION}/checksums.txt"

tmp=$(mktemp -d)
staged=""
staged_version=""
cleanup() {
  rm -rf "$tmp"
  [ -z "$staged" ] || rm -f "$staged"
  [ -z "$staged_version" ] || rm -f "$staged_version"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

step "Downloading ${asset}"
if ! curl -fsSL --connect-timeout 10 --max-time 300 "$asset_url" -o "$tmp/$asset"; then
  err "failed to download ${asset_url} (no release for ${os}/${arch}?)"
fi

# ---------------------------------------------------------------------------
# Verify checksum (sha256)
# ---------------------------------------------------------------------------
if ! curl -fsSL --connect-timeout 10 --max-time 30 "$checksum_url" -o "$tmp/checksums.txt" 2>/dev/null; then
  err "failed to download checksums.txt; refusing to install without checksum verification"
fi

expected=$(awk -v asset="$asset" '$2 == asset && length($1) == 64 && $1 ~ /^[0-9a-fA-F]+$/ { count++; hash=$1 } END { if (count == 1) print hash }' "$tmp/checksums.txt")
if [ -z "$expected" ]; then
  err "${asset} not listed in checksums.txt; refusing to install without checksum verification"
fi

if has sha256sum; then
  actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
elif has shasum; then
  actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
else
  err "no sha256 tool found; install sha256sum or shasum and retry"
fi

if [ "$expected" != "$actual" ]; then
  err "checksum mismatch (expected=${expected} got=${actual})"
fi
step "Verified checksum"

# ---------------------------------------------------------------------------
# Extract and install
# ---------------------------------------------------------------------------
archive_names="$tmp/archive-names.txt"
archive_verbose="$tmp/archive-verbose.txt"
if ! tar -tzf "$tmp/$asset" > "$archive_names" 2>/dev/null; then
  err "invalid release archive"
fi

license_count=0
notice_count=0
readme_count=0
installer_count=0
binary_count=0
while IFS= read -r entry; do
  case "$entry" in
    LICENSE) license_count=$((license_count + 1)) ;;
    NOTICE.md) notice_count=$((notice_count + 1)) ;;
    README.md) readme_count=$((readme_count + 1)) ;;
    install.sh) installer_count=$((installer_count + 1)) ;;
    wavo) binary_count=$((binary_count + 1)) ;;
    *) err "release archive contains an unexpected or unsafe path" ;;
  esac
done < "$archive_names"

if [ "$license_count" -ne 1 ] || [ "$notice_count" -ne 1 ] || \
   [ "$readme_count" -ne 1 ] || [ "$installer_count" -ne 1 ] || \
   [ "$binary_count" -ne 1 ]; then
  err "release archive does not contain the exact expected files"
fi
if ! LC_ALL=C tar -tvzf "$tmp/$asset" > "$archive_verbose" 2>/dev/null ||
   ! awk 'substr($0, 1, 1) != "-" { exit 1 } END { if (NR != 5) exit 1 }' "$archive_verbose"; then
  err "release archive contains a non-regular entry"
fi
if ! tar -xOzf "$tmp/$asset" "$BINARY" > "$tmp/$BINARY.extracted" 2>/dev/null; then
  err "could not read the Wavo binary from the release archive"
fi
if [ ! -f "$tmp/$BINARY.extracted" ] || [ -L "$tmp/$BINARY.extracted" ]; then
  err "archive did not contain a regular '$BINARY' binary"
fi

mkdir -p "$INSTALL_DIR"
staged=$(mktemp "$INSTALL_DIR/.${BINARY}.tmp.XXXXXX")
staged_version=$(mktemp "$INSTALL_DIR/.${BINARY}-version.tmp.XXXXXX")
if has install; then
  install -m 0755 "$tmp/$BINARY.extracted" "$staged"
else
  cp "$tmp/$BINARY.extracted" "$staged"
  chmod 0755 "$staged"
fi
printf '%s\n' "$VERSION" > "$staged_version"
chmod 0644 "$staged_version"
mv -f "$staged" "$INSTALL_DIR/$BINARY"
staged=""
mv -f "$staged_version" "$version_path"
staged_version=""
if [ ! -f "$existing_path" ] || [ -L "$existing_path" ] || [ ! -x "$existing_path" ]; then
  err "installed Wavo target failed final verification"
fi
if [ ! -f "$version_path" ] || [ -L "$version_path" ] ||
   [ "$(awk 'NR == 1 { value=$0 } NR > 1 { invalid=1 } END { if (!invalid) print value }' "$version_path")" != "$VERSION" ]; then
  err "installed Wavo version record failed final verification"
fi

case "$install_mode" in
  reinstall) ok "Reinstalled ${C_BOLD}${BINARY} ${VERSION}${C_RESET} to ${INSTALL_DIR}/${BINARY}" ;;
  update)    ok "Installed ${C_BOLD}${BINARY} ${VERSION}${C_RESET} over the existing binary at ${INSTALL_DIR}/${BINARY}" ;;
  *)         ok "Installed ${C_BOLD}${BINARY} ${VERSION}${C_RESET} to ${INSTALL_DIR}/${BINARY}" ;;
esac

# ---------------------------------------------------------------------------
# PATH hint
# ---------------------------------------------------------------------------
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    printf '\n'
    warn "${INSTALL_DIR} is not on your PATH"
    printf '    add this to your shell profile (~/.zshrc, ~/.bashrc, etc.):\n\n'
    # shellcheck disable=SC2016  # literal $PATH is intentional here
    printf '      %sexport PATH="%s:$PATH"%s\n\n' "$C_DIM" "$INSTALL_DIR" "$C_RESET"
    ;;
esac

# ---------------------------------------------------------------------------
# Optional: set up tab completion (interactive only)
# ---------------------------------------------------------------------------
# Skip silently when not running on a real terminal (e.g. CI, Dockerfile).
# stdout is checked because `curl ... | sh` keeps it as a TTY but rebinds stdin
# to the curl pipe — we read input from /dev/tty instead.
print_next_steps() {
  printf '\n  %sNext steps%s\n' "$C_BOLD" "$C_RESET"
  if [ ! -f "$HOME/.wavo/config.yaml" ]; then
    printf '    %swavo auth login%s        log in to your Wavo instance\n' "$C_CYAN" "$C_RESET"
  fi
  printf '    %swavo --help%s            see all commands\n\n' "$C_CYAN" "$C_RESET"
}

if [ ! -t 1 ] || [ ! -r /dev/tty ]; then
  exit 0
fi

case "${SHELL##*/}" in
  bash) shell_kind=bash ;;
  zsh)  shell_kind=zsh ;;
  fish) shell_kind=fish ;;
  *)
    printf '\n'
    step "Tab completion: run '${C_BOLD}${BINARY} completion --help${C_RESET}' to set up"
    print_next_steps
    exit 0
    ;;
esac

# Skip the prompt entirely if completion is already configured.
completion_configured=false
case "$shell_kind" in
  bash)
    completion_dir="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
    completion_path="$completion_dir/$BINARY"
    [ -f "$completion_path" ] && completion_configured=true
    ;;
  fish)
    completion_dir="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
    completion_path="$completion_dir/$BINARY.fish"
    [ -f "$completion_path" ] && completion_configured=true
    ;;
  zsh)
    rc="${ZDOTDIR:-$HOME}/.zshrc"
    completion_path="$rc"
    zsh_line="source <(\"$INSTALL_DIR/$BINARY\" completion zsh -c)"
    if [ -f "$rc" ] && grep -Fq "$zsh_line" "$rc"; then
      completion_configured=true
    fi
    ;;
esac

if [ "$completion_configured" = true ]; then
  printf '\n'
  step "${shell_kind} completion already configured in ${completion_path} — skipping"
else
  printf '\n  %sSet up tab completion for %s?%s [Y/n] ' "$C_BOLD" "$shell_kind" "$C_RESET"
  if ! read -r response < /dev/tty; then
    echo
    response=n
  fi
  case "$response" in
    n|N|no|NO|No)
      step "Skipped completion setup"
      ;;
    *)
      case "$shell_kind" in
        bash)
          mkdir -p "$completion_dir"
          "$INSTALL_DIR/$BINARY" completion bash -c > "$completion_path"
          ok "Added bash completion to ${completion_path} — restart your shell to enable"
          ;;
        fish)
          mkdir -p "$completion_dir"
          "$INSTALL_DIR/$BINARY" completion fish -c > "$completion_path"
          ok "Added fish completion to ${completion_path}"
          ;;
        zsh)
          printf '\n# Wavo CLI completion\n%s\n' "$zsh_line" >> "$rc"
          ok "Added zsh completion to ${rc} — restart your shell to enable"
          ;;
      esac
      ;;
  esac
fi

# ---------------------------------------------------------------------------
# Optional: 'wv' alias
# ---------------------------------------------------------------------------
case "$shell_kind" in
  bash)
    alias_rc="$HOME/.bashrc"
    alias_line="alias wv='$BINARY'"
    ;;
  zsh)
    alias_rc="${ZDOTDIR:-$HOME}/.zshrc"
    alias_line="alias wv='$BINARY'"
    ;;
  fish)
    alias_rc="${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
    alias_line="alias wv '$BINARY'"
    ;;
esac

alias_configured=false
[ -f "$alias_rc" ] && grep -Fq "$alias_line" "$alias_rc" && alias_configured=true

if [ "$alias_configured" = true ]; then
  step "'wv' alias already configured in ${alias_rc} — skipping"
else
  printf "\n  %sCreate 'wv' alias for %s?%s [Y/n] " "$C_BOLD" "$BINARY" "$C_RESET"
  if ! read -r response < /dev/tty; then
    echo
    response=n
  fi
  case "$response" in
    n|N|no|NO|No)
      step "Skipped 'wv' alias setup"
      ;;
    *)
      mkdir -p "$(dirname "$alias_rc")"
      printf '\n# Wavo CLI alias\n%s\n' "$alias_line" >> "$alias_rc"
      ok "Added 'wv' alias to ${alias_rc} — restart your shell to enable"
      ;;
  esac
fi

print_next_steps
