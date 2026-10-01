#!/bin/sh
# The fresh-machine install check for macOS, like run.sh does on a fresh
# Fedora: install → a new zsh records commands → install again → uninstall.
# It runs as a brand-new Mac user would (zsh login shell, no ~/.zshrc yet) in
# a temporary home directory, so it never touches your own. CI runs it on
# GitHub's macOS machines; it also works on Linux with zsh.
#
#   LOXX_RELEASE_URL=https://github.com/AniketR10/loxx/releases/download/<tag> sh tools/installtest/macos.sh
#   make dist && LOXX_RELEASE_URL=file://$PWD/dist sh tools/installtest/macos.sh
set -eu
: "${LOXX_RELEASE_URL:?set LOXX_RELEASE_URL to a release, or to file://…/dist after make dist}"
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

home=$(mktemp -d)
trap 'rm -rf "$home"' EXIT
export HOME="$home" SHELL=/bin/zsh
unset ZDOTDIR XDG_DATA_HOME XDG_STATE_HOME LOXX_DB_PATH LOXX_BACKGROUND_EMBED
cd "$home"
loxx=$home/.local/bin/loxx

# The installer comes from the release itself, as a user would get it.
echo "== install from $LOXX_RELEASE_URL (the only step)"
curl -fsSL "$LOXX_RELEASE_URL/install.sh" -o "$home/install.sh"
sh "$home/install.sh"
[ -f ~/.zshrc ] || fail "setup did not create ~/.zshrc for a zsh user"

echo "== a new interactive zsh runs commands"
# The shell exits with its last command's status (false: 1); that is not a
# failure of the check, so ignore it.
zsh -i >/dev/null 2>&1 <<'CMDS' || true
echo hello-from-fresh-mac
false
CMDS
sleep 3
found=$("$loxx" search hello-from-fresh)
echo "$found"
echo "$found" | grep -q 'exit 0' || fail "the command was not recorded"
"$loxx" search false | grep -q 'exit 1' || fail "the failing command's exit code was not recorded"

echo "== installing again"
sh "$home/install.sh" 2>&1 | grep -v '^loxx install' || true
[ "$(grep -c '>>> loxx >>>' ~/.zshrc)" = 1 ] || fail "installing twice added a second block"
"$loxx" status | grep -q '2 unique · 2 runs recorded live, 0 imported' ||
  fail "installing again changed the history (double import?)"

echo "== uninstall"
"$loxx" uninstall --delete-data
[ ! -e ~/.zshrc ] || fail "the ~/.zshrc that setup created was not removed"
[ ! -e "$loxx" ] || fail "the binary was not removed"
echo "PASS: install → record → re-install → uninstall as a fresh $(uname -s) zsh user"
