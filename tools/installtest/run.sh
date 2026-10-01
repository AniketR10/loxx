#!/bin/sh
# Dev-only: the install check on a fresh Fedora container (needs podman):
# install (the only step), record commands in a new shell, re-install,
# uninstall, and compare ~/.bashrc byte for byte.
#
#   sh tools/installtest/run.sh
#       builds loxx and serves it to the container as a local "release"
#   LOXX_RELEASE_URL=https://github.com/AniketR10/loxx/releases/download/v0.1.0 sh tools/installtest/run.sh
#       tests a real published release: its install.sh and binaries
set -eu
cd "$(dirname "$0")/../.."
if [ -n "${LOXX_RELEASE_URL:-}" ]; then
  exec podman run --rm -e LOXX_RELEASE_URL \
    -v "$PWD/tools/installtest/in-container.sh:/test.sh:ro,Z" \
    registry.fedoraproject.org/fedora:43 sh /test.sh
fi
make -s build >/dev/null
release=$(mktemp -d)
trap 'rm -rf "$release"' EXIT INT TERM
asset=loxx-linux-$(go env GOARCH)
cp bin/loxx "$release/$asset"
cp install.sh "$release/install.sh"
(cd "$release" && sha256sum "$asset" >checksums.txt)
podman run --rm -e LOXX_RELEASE_URL=file:///release \
  -v "$release:/release:ro,Z" \
  -v "$PWD/tools/installtest/in-container.sh:/test.sh:ro,Z" \
  registry.fedoraproject.org/fedora:43 sh /test.sh
