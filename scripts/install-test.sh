#!/usr/bin/env sh
set -eu

repo=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT HUP INT TERM

fail() { printf 'not ok - %s\n' "$*" >&2; exit 1; }
ok() { printf 'ok - %s\n' "$*"; }

tools="$root/tools"
mkdir -p "$tools"
for tool in awk chmod cp grep gzip head mkdir mktemp mv rm shasum sha256sum tar tr; do
  path=$(command -v "$tool" 2>/dev/null || true)
  [ -z "$path" ] || ln -s "$path" "$tools/$tool"
done

cat > "$tools/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) printf '%s\n' "${MOCK_OS:-Linux}" ;;
  -m) printf '%s\n' "${MOCK_ARCH:-x86_64}" ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$tools/uname"

cat > "$tools/curl" <<'EOF'
#!/bin/sh
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    -H|--connect-timeout|--max-time|--max-filesize|-w) shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
printf '%s\n' "$url" >> "$MOCK_CURL_LOG"
case "$url" in
  *'/releases?per_page=1')
    printf '[{"tag_name":"%s","prerelease":true}]\n' "$MOCK_TAG"
    ;;
  */checksums.txt)
    asset=${MOCK_ASSET_NAME:?}
    if [ "${MOCK_BAD_CHECKSUM:-0}" = 1 ]; then
      hash=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    else
      if command -v sha256sum >/dev/null 2>&1; then
        hash=$(sha256sum "$MOCK_ARCHIVE" | awk '{print $1}')
      else
        hash=$(shasum -a 256 "$MOCK_ARCHIVE" | awk '{print $1}')
      fi
    fi
    printf '%s  %s\n' "$hash" "$asset" > "$output"
    ;;
  */wavo_*.tar.gz)
    cp "$MOCK_ARCHIVE" "$output"
    ;;
  *) exit 22 ;;
esac
EOF
chmod 0755 "$tools/curl"

file_mode() {
  if stat -f '%Lp' "$1" >/dev/null 2>&1; then
    stat -f '%Lp' "$1"
  else
    stat -c '%a' "$1"
  fi
}

archive_generator="$root/archive-generator.go"
cat > "$archive_generator" <<'EOF'
package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: archive-generator OUTPUT KIND")
	}
	file, err := os.Create(os.Args[1])
	if err != nil { panic(err) }
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	regular := func(name, body string) {
		data := []byte(body)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil { panic(err) }
		if _, err := tw.Write(data); err != nil { panic(err) }
	}
	regular("LICENSE", "license\n")
	regular("NOTICE.md", "notice\n")
	regular("README.md", "readme\n")
	regular("install.sh", "installer\n")
	kind := os.Args[2]
	switch kind {
	case "symlink":
		if err := tw.WriteHeader(&tar.Header{Name: "wavo", Mode: 0777, Typeflag: tar.TypeSymlink, Linkname: "LICENSE"}); err != nil { panic(err) }
	case "hardlink":
		if err := tw.WriteHeader(&tar.Header{Name: "wavo", Mode: 0644, Typeflag: tar.TypeLink, Linkname: "LICENSE"}); err != nil { panic(err) }
	default:
		regular("wavo", "new-wavo\n")
	}
	extra := map[string]string{"extra":"extra", "traversal":"../escape", "absolute":"/absolute", "backslash":`bad\path`}
	if name := extra[kind]; name != "" { regular(name, "unsafe\n") }
	if err := tw.Close(); err != nil { panic(err) }
	if err := gz.Close(); err != nil { panic(err) }
	if err := file.Close(); err != nil { panic(err) }
}
EOF

make_archive() {
  output=$1
  kind=$2
  GO111MODULE=off GOWORK=off go run "$archive_generator" "$output" "$kind"
}

run_install() {
  case_name=$1
  version=$2
  os=$3
  arch=$4
  archive=$5
  install_dir="$root/$case_name/bin"
  home="$root/$case_name/home"
  mkdir -p "$install_dir" "$home"
  curl_log="$root/$case_name/curl.log"
  : > "$curl_log"
  clean_version=${version#v}
  [ "$version" = latest ] && clean_version=${MOCK_TAG#v}
  asset="wavo_${clean_version}_${os}_$([ "$arch" = x86_64 ] && printf x86_64 || printf arm64).tar.gz"
  HOME="$home" PATH="$tools" NO_COLOR=1 \
    WAVO_INSTALL_DIR="$install_dir" WAVO_VERSION="$version" \
    MOCK_OS="$os" MOCK_ARCH="$arch" MOCK_ARCHIVE="$archive" \
    MOCK_ASSET_NAME="$asset" MOCK_CURL_LOG="$curl_log" \
    /bin/sh "$repo/install.sh"
}

make_archive "$root/regular.tar.gz" regular
MOCK_TAG=v0.1.0-beta.2
export MOCK_TAG

run_install pinned v0.1.0-beta.1 Linux x86_64 "$root/regular.tar.gz" >/dev/null
[ "$(cat "$root/pinned/bin/wavo")" = new-wavo ] || fail "pinned beta did not replace the binary"
[ "$(file_mode "$root/pinned/bin/wavo")" = 755 ] || fail "fallback mode is not exactly 0755"
[ ! -d "$root/pinned/bin/wavo" ] || fail "successful replacement created a nested binary"
grep -q '/v0.1.0-beta.1/wavo_0.1.0-beta.1_Linux_x86_64.tar.gz$' "$root/pinned/curl.log" || fail "Linux asset mapping is wrong"
ok "pinned beta installs atomically with fallback mode 0755"

run_install latest latest Darwin arm64 "$root/regular.tar.gz" >/dev/null
grep -q '/releases?per_page=1$' "$root/latest/curl.log" || fail "latest channel did not use the releases collection"
grep -q '/v0.1.0-beta.2/wavo_0.1.0-beta.2_Darwin_arm64.tar.gz$' "$root/latest/curl.log" || fail "macOS asset mapping is wrong"
ok "latest prerelease and macOS arm64 mapping pass"

for invalid in '0.1.0' 'v01.2.3' 'v1.2.3-01' 'v1.2.3/evil' 'v1.2.3*' 'v1.2.3 bad'; do
  if run_install invalid-env "$invalid" Linux x86_64 "$root/regular.tar.gz" >/dev/null 2>&1; then
    fail "unsafe WAVO_VERSION was accepted: $invalid"
  fi
done
ok "unsafe environment release tags are rejected"

MOCK_TAG='v0.1.0/../../payload'
export MOCK_TAG
if run_install invalid-api latest Linux x86_64 "$root/regular.tar.gz" >/dev/null 2>&1; then
  fail "unsafe API release tag was accepted"
fi
[ "$(wc -l < "$root/invalid-api/curl.log" | tr -d ' ')" = 1 ] || fail "unsafe API tag reached an asset URL"
ok "unsafe API release tags are rejected before URL construction"
MOCK_TAG=v0.1.0-beta.2
export MOCK_TAG

mkdir -p "$root/checksum/bin" "$root/checksum/home"
printf 'old-wavo\n' > "$root/checksum/bin/wavo"
chmod 0755 "$root/checksum/bin/wavo"
if MOCK_BAD_CHECKSUM=1 run_install checksum v0.1.0-beta.1 Linux x86_64 "$root/regular.tar.gz" >/dev/null 2>&1; then
  fail "checksum mismatch was accepted"
fi
[ "$(cat "$root/checksum/bin/wavo")" = old-wavo ] || fail "checksum failure replaced the existing binary"
ok "checksum failure preserves the existing binary"

for kind in extra traversal absolute backslash symlink hardlink; do
  make_archive "$root/$kind.tar.gz" "$kind"
  mkdir -p "$root/$kind/bin" "$root/$kind/home"
  printf 'old-wavo\n' > "$root/$kind/bin/wavo"
  chmod 0755 "$root/$kind/bin/wavo"
  if run_install "$kind" v0.1.0-beta.1 Linux x86_64 "$root/$kind.tar.gz" >/dev/null 2>&1; then
    fail "$kind archive was accepted"
  fi
  [ "$(cat "$root/$kind/bin/wavo")" = old-wavo ] || fail "$kind archive replaced the existing binary"
done
ok "extra, absolute, traversal, backslash, symlink, and hardlink entries are rejected"

mkdir -p "$root/directory-target/bin/wavo" "$root/directory-target/home"
if run_install directory-target v0.1.0-beta.1 Linux x86_64 "$root/regular.tar.gz" >/dev/null 2>&1; then
  fail "directory target was accepted"
fi
[ -d "$root/directory-target/bin/wavo" ] || fail "directory target was not preserved"
[ -z "$(ls -A "$root/directory-target/bin/wavo")" ] || fail "installer nested a file inside the directory target"
ok "directory target is rejected and preserved"

mkdir -p "$root/replacement/bin" "$root/replacement/home"
printf 'old-wavo\n' > "$root/replacement/bin/wavo"
printf 'v0.2.0\n' > "$root/replacement/bin/.wavo-version"
chmod 0700 "$root/replacement/bin/wavo"
run_install replacement v0.1.0-beta.1 Linux x86_64 "$root/regular.tar.gz" >/dev/null
[ "$(cat "$root/replacement/bin/wavo")" = new-wavo ] || fail "explicit replacement did not update content"
[ "$(file_mode "$root/replacement/bin/wavo")" = 755 ] || fail "explicit replacement mode is not exactly 0755"
[ ! -d "$root/replacement/bin/wavo" ] || fail "explicit replacement nested the binary"
ok "explicit version atomically replaces an existing binary and may intentionally downgrade"

mkdir -p "$root/downgrade/bin" "$root/downgrade/home"
printf 'old-wavo\n' > "$root/downgrade/bin/wavo"
printf 'v0.2.0\n' > "$root/downgrade/bin/.wavo-version"
chmod 0755 "$root/downgrade/bin/wavo"
MOCK_TAG=v0.1.0
export MOCK_TAG
if run_install downgrade latest Linux x86_64 "$root/regular.tar.gz" >/dev/null 2>&1; then
  fail "implicit latest downgrade was accepted"
fi
[ "$(cat "$root/downgrade/bin/wavo")" = old-wavo ] || fail "implicit downgrade changed the existing binary"
ok "implicit latest downgrade is rejected without executing the existing binary"

mkdir -p "$root/upgrade/bin" "$root/upgrade/home"
printf 'old-wavo\n' > "$root/upgrade/bin/wavo"
printf 'v0.1.0-beta.1\n' > "$root/upgrade/bin/.wavo-version"
chmod 0755 "$root/upgrade/bin/wavo"
MOCK_TAG=v0.1.0-beta.2
export MOCK_TAG
run_install upgrade latest Linux x86_64 "$root/regular.tar.gz" >/dev/null
[ "$(cat "$root/upgrade/bin/wavo")" = new-wavo ] || fail "implicit latest upgrade did not replace the binary"
[ "$(cat "$root/upgrade/bin/.wavo-version")" = v0.1.0-beta.2 ] || fail "version record was not updated"
ok "implicit latest upgrade uses the trusted version record"
