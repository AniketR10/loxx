#!/bin/sh
# Runs inside a fresh Fedora container (see run.sh). Exits non-zero on the
# first failed check.
set -eu
cd ~
fail() {
  echo "FAIL: $*" >&2
  exit 1
}
cp ~/.bashrc /tmp/bashrc.orig
! command -v loxx >/dev/null || fail "loxx is already installed"

# The installer comes from the release itself, as a user would get it.
echo "== install from $LOXX_RELEASE_URL (the only step)"
curl -fsSL "$LOXX_RELEASE_URL/install.sh" -o /tmp/install.sh
sh /tmp/install.sh

echo "== a new interactive shell runs commands"
# The shell exits with its last command's status (false: 1); that is not a
# failure of the check, so ignore it.
bash -i >/dev/null 2>&1 <<'CMDS' || true
echo hello-from-fresh-fedora
false
CMDS
sleep 3
found=$(~/.local/bin/loxx search hello-from-fresh)
echo "$found"
echo "$found" | grep -q 'exit 0' || fail "the command was not recorded"
~/.local/bin/loxx search false | grep -q 'exit 1' || fail "the failing command's exit code was not recorded"

echo "== installing again"
sh /tmp/install.sh 2>&1 | grep -v '^loxx install' || true
[ "$(grep -c '>>> loxx >>>' ~/.bashrc)" = 1 ] || fail "installing twice added a second block"
~/.local/bin/loxx status | grep -q '2 unique · 2 runs recorded live, 0 imported' ||
  fail "installing again changed the history (double import?)"

echo "== uninstall"
~/.local/bin/loxx uninstall --delete-data
[ "$(sha256sum </tmp/bashrc.orig)" = "$(sha256sum <~/.bashrc)" ] || fail "~/.bashrc differs from before the install"
[ ! -e ~/.local/bin/loxx ] || fail "the binary was not removed"
echo "PASS: install → record → re-install → uninstall on a fresh Fedora"
