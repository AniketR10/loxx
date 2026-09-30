#!/bin/sh
# loxx installer:
#
#   curl -fsSL https://github.com/AniketR10/loxx/releases/latest/download/install.sh | sh
#
# Downloads the loxx binary for this machine, checks its SHA-256 against the
# release's checksums.txt, installs it to ~/.local/bin/loxx and runs
# `loxx setup`, which adds the hook to your shell's rc file and imports your
# existing history. Needs no root. `loxx uninstall` undoes all of it.
#
# Environment:
#   LOXX_RELEASE_URL  where to download from (default: the latest GitHub release)
#   LOXX_BIN_DIR      where to install the binary (default: ~/.local/bin)
set -eu

release_url=${LOXX_RELEASE_URL:-https://github.com/AniketR10/loxx/releases/latest/download}
bin_dir=${LOXX_BIN_DIR:-$HOME/.local/bin}
state_dir=${XDG_STATE_HOME:-$HOME/.local/state}/loxx

say() { printf 'loxx install: %s\n' "$*" >&2; }
fail() {
  say "$*"
  exit 1
}

[ "$(uname -s)" = Linux ] || fail "loxx supports Linux only for now (this is $(uname -s))"
case $(uname -m) in
  (x86_64 | amd64) arch=amd64 ;;
  (aarch64 | arm64) arch=arm64 ;;
  (*) fail "unsupported CPU architecture: $(uname -m)" ;;
esac
asset=loxx-linux-$arch

if command -v curl >/dev/null 2>&1; then
  download() { curl -fsSL --retry 3 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  download() { wget -q -O "$2" "$1"; }
else
  fail "needs curl or wget"
fi
command -v sha256sum >/dev/null 2>&1 || fail "needs sha256sum (from coreutils)"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $asset"
download "$release_url/$asset" "$tmp/$asset" || fail "could not download $release_url/$asset"
download "$release_url/checksums.txt" "$tmp/checksums.txt" || fail "could not download $release_url/checksums.txt"

# sha256sum writes "<hash>  <file>" (or "<hash> *<file>" in binary mode).
expected=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt has no entry for $asset"
actual=$(sha256sum "$tmp/$asset" | cut -d ' ' -f 1)
[ "$actual" = "$expected" ] || fail "checksum mismatch for $asset (expected $expected, got $actual); nothing was installed"

mkdir -p "$bin_dir" "$state_dir"
bin_dir=$(cd "$bin_dir" && pwd)
chmod 0755 "$tmp/$asset"
# Copy next to the target first, then rename: replacing the binary is atomic,
# even while an older loxx is running.
cp "$tmp/$asset" "$bin_dir/.loxx.new"
mv -f "$bin_dir/.loxx.new" "$bin_dir/loxx"
# Tell `loxx uninstall` that this binary is ours to remove.
printf '%s\n' "$bin_dir/loxx" >"$state_dir/installed-binary"
say "installed $bin_dir/loxx"

"$bin_dir/loxx" setup
