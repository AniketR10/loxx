#!/bin/sh
# Checks core principle P4 for each loxx binary given: one file that runs
# without anything else installed.
#
# - Linux: the binary must be statically linked.
# - macOS: Apple doesn't support fully static binaries; every program links
#   macOS's own system library. So the binary must be built without cgo, and
#   (where otool exists, i.e. on a Mac) link only libraries that are part of
#   macOS itself.
set -eu

for bin; do
  case $(file -b "$bin") in
    (ELF*)
      file -b "$bin" | grep -q 'statically linked' || {
        file "$bin"
        echo "$bin is not statically linked"
        exit 1
      } ;;
    (Mach-O*)
      go version -m "$bin" | grep -q 'CGO_ENABLED=0' || {
        echo "$bin was built with cgo"
        exit 1
      }
      if command -v otool >/dev/null 2>&1; then
        other=$(otool -L "$bin" | tail -n +2 | awk '{ print $1 }' | grep -v -e '^/usr/lib/' -e '^/System/Library/' || true)
        [ -z "$other" ] || {
          echo "$bin links libraries that are not part of macOS: $other"
          exit 1
        }
      fi ;;
    (*)
      echo "$bin is not a Linux or macOS binary: $(file -b "$bin")"
      exit 1 ;;
  esac
  echo "$bin: one self-contained file"
done
