#!/bin/sh
# Install check on a fresh Fedora container (needs podman). Set LOXX_RELEASE_URL
# to test a published release instead of a local build.
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
