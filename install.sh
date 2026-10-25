#!/bin/sh
# loxx installer: downloads and verifies the binary, installs it to
# ~/.local/bin and runs `loxx setup`. Env: LOXX_RELEASE_URL, LOXX_BIN_DIR.
set -eu

release_url=${LOXX_RELEASE_URL:-https://github.com/AniketR10/loxx/releases/latest/download}
bin_dir=${LOXX_BIN_DIR:-$HOME/.local/bin}
state_dir=${XDG_STATE_HOME:-$HOME/.local/state}/loxx

say() { printf 'loxx install: %s\n' "$*" >&2; }
fail() {
  say "$*"
  exit 1
}

case $(uname -s) in
  (Linux) os=linux ;;
  (Darwin) os=darwin ;;
  (*) fail "loxx supports Linux and macOS only (this is $(uname -s))" ;;
esac
case $(uname -m) in
  (x86_64 | amd64) arch=amd64 ;;
  (aarch64 | arm64) arch=arm64 ;;
  (*) fail "unsupported CPU architecture: $(uname -m)" ;;
esac
asset=loxx-$os-$arch

if command -v curl >/dev/null 2>&1; then
  fetcher=curl
elif command -v wget >/dev/null 2>&1; then
  fetcher=wget
else
  fail "needs curl or wget"
fi
download() {
  if [ "$fetcher" = curl ]; then
    curl -fsSL --retry 3 -o "$2" "$1"
  else
    wget -q -O "$2" "$1"
  fi
}
# Linux has sha256sum; macOS has shasum.
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
  fail "needs sha256sum or shasum"
fi

tmp=$(mktemp -d)
pid=''
# Stops a download still running and removes the temp files. The signal traps
# call it too: zsh skips the EXIT trap when a trap calls exit.
cleanup() {
  [ -z "$pid" ] || kill "$pid" 2>/dev/null
  pid=''
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

key=Ctrl-Alt-G
[ "$os" = darwin ] && key=Ctrl-Option-G
# Shown under the progress bar, a new one every 3 s: jokes and tips, in turn.
quips="teaching loxx to understand plain English...
tip: type \`loxx\` or press $key to search your history
loxx remembers your commands, so you don't have to...
tip: start a command with a space and loxx won't save it
hiding your passwords before loxx saves anything...
tip: \`loxx forget --last\` deletes the command you just ran
packing a small brain that works offline...
tip: describe it, like \"free up disk space\" or \"undo my last commit\"
loxx finds the command, you press Enter. deal?
tip: everything stays on this computer; loxx never uses the internet"
nquips=$(printf '%s\n' "$quips" | wc -l)

# progress PID FILE HEADERS: until PID exits, draws a bar and a quip on two
# lines of the terminal, then clears the quip line.
progress() {
  case ${LC_ALL:-${LC_CTYPE:-${LANG:-}}} in
    (*[Uu][Tt][Ff]-8* | *[Uu][Tt][Ff]8*) full='█' empty='░' ;;
    (*) full='#' empty='-' ;;
  esac
  cols=$(stty size </dev/tty 2>/dev/null | cut -d ' ' -f 2)
  [ "${cols:-0}" -gt 20 ] 2>/dev/null || cols=80
  frame=0
  sec=$(date +%S)
  quip=$((${sec#0} % nquips))
  printf '\n' >&2
  while :; do
    running=1
    kill -0 "$1" 2>/dev/null || running=0
    got=$(wc -c 2>/dev/null <"$2" || echo 0)
    # The file's size: the Content-Length of a 2xx response, not of a redirect.
    total=$(awk '/^HTTP\// { ok = $2 ~ /^2/; n = 0 } ok && tolower($1) == "content-length:" { n = $2 } END { print n + 0 }' "$3" 2>/dev/null || echo 0)
    if [ "$total" -gt 0 ]; then
      pct=$((got * 100 / total))
      [ "$pct" -le 100 ] || pct=100
      bar='' i=0
      while [ $i -lt 24 ]; do
        if [ $((i * 100 / 24)) -lt "$pct" ]; then bar=$bar$full; else bar=$bar$empty; fi
        i=$((i + 1))
      done
      line=$(printf '  [%s] %3d%%  %d/%d MB' "$bar" "$pct" $((got / 1000000)) $((total / 1000000)))
    else
      line=$(printf '  %d MB' $((got / 1000000)))
    fi
    text=$(printf '%s\n' "$quips" | sed -n "$((quip + 1))p" | cut -c "1-$((cols - 3))")
    [ "$running" = 1 ] || text=''
    # Up to the bar line, redraw both lines, back to the start of the quip line.
    printf '\033[1A\r%s\033[K\n  %s\033[K\r' "$line" "$text" >&2
    [ "$running" = 1 ] || return 0
    sleep 0.2 2>/dev/null || sleep 1
    frame=$((frame + 1))
    [ $((frame % 15)) -ne 0 ] || quip=$(((quip + 1) % nquips))
  done
}

say "downloading $asset"
if [ -t 2 ]; then
  # Started directly, not through download(), so $pid is the downloader itself
  # and the exit trap can stop it. curl's -D saves the response headers as soon
  # as they arrive, which tells the progress bar the size.
  if [ "$fetcher" = curl ]; then
    curl -fsSL --retry 3 -D "$tmp/headers" -o "$tmp/$asset" "$release_url/$asset" &
  else
    wget -q -O "$tmp/$asset" "$release_url/$asset" &
  fi
  pid=$!
  progress "$pid" "$tmp/$asset" "$tmp/headers"
  if ! wait "$pid"; then
    pid=''
    printf '\033[1A\r\033[K' >&2 # the bar
    fail "could not download $release_url/$asset"
  fi
  pid=''
else
  download "$release_url/$asset" "$tmp/$asset" || fail "could not download $release_url/$asset"
fi
download "$release_url/checksums.txt" "$tmp/checksums.txt" || fail "could not download $release_url/checksums.txt"

# sha256sum writes "<hash>  <file>" (or "<hash> *<file>" in binary mode).
expected=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt has no entry for $asset"
actual=$(sha256 "$tmp/$asset")
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
# The hook only loads in new shells; this one is still the old one.
case ${SHELL##*/} in
  (zsh | bash) say "to use it in this terminal right away, run: exec ${SHELL##*/}" ;;
esac
