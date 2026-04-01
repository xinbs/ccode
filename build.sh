#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$ROOT_DIR/dist"
APP_NAME="ccode"
VERSION="${VERSION:-$(tr -d '[:space:]' < "$ROOT_DIR/VERSION")}"

TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

LDFLAGS=(
  "-s"
  "-w"
  "-X"
  "main.version=$VERSION"
  "-X"
  "main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  "-X"
  "main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
)

for target in "${TARGETS[@]}"; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  EXT=""
  if [[ "$GOOS" == "windows" ]]; then
    EXT=".exe"
  fi

  OUT="$DIST_DIR/${APP_NAME}-${VERSION}-${GOOS}-${GOARCH}${EXT}"
  echo "building $OUT"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "${LDFLAGS[*]}" -o "$OUT" .
done

(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./* > SHA256SUMS.txt
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 ./* > SHA256SUMS.txt
  else
    echo "warning: no sha256 tool found, skipping checksum generation" >&2
  fi
)

echo "artifacts written to $DIST_DIR"
