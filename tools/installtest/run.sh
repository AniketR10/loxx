#!/bin/sh
# Dev-only: the Phase 6 exit check. Builds loxx, serves it as a local "release"
# to a fresh Fedora container, and runs in-container.sh there: install with
# install.sh (the only step), record commands in a new shell, re-install,
# uninstall, and compare ~/.bashrc byte for byte. Needs podman.
#
#   sh tools/installtest/run.sh
set -eu
cd "$(dirname "$0")/../.."
make -s build >/dev/null
release=$(mktemp -d)
trap 'rm -rf "$release"' EXIT INT TERM
asset=loxx-linux-$(go env GOARCH)
cp bin/loxx "$release/$asset"
(cd "$release" && sha256sum "$asset" >checksums.txt)
podman run --rm \
  -v "$release:/release:ro,Z" \
  -v "$PWD/install.sh:/install.sh:ro,Z" \
  -v "$PWD/tools/installtest/in-container.sh:/test.sh:ro,Z" \
  registry.fedoraproject.org/fedora:43 sh /test.sh
