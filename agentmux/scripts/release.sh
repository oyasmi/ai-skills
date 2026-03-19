#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-$ROOT_DIR/dist}"
VERSION="${VERSION:-dev}"
APP_NAME="agentmux"

TARGETS=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
)

export GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
export GOPATH="${GOPATH:-$ROOT_DIR/.cache/go-path}"
export GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}"

mkdir -p "$GOCACHE" "$GOPATH" "$GOMODCACHE"
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

cp "$ROOT_DIR/README.md" "$DIST_DIR/README.md"

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<<"$target"
  BUILD_DIR="$DIST_DIR/${APP_NAME}_${VERSION}_${GOOS}_${GOARCH}"
  mkdir -p "$BUILD_DIR"

  echo "Building $GOOS/$GOARCH..."
  GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o "$BUILD_DIR/$APP_NAME" "$ROOT_DIR/cmd/agentmux"

  cp "$ROOT_DIR/examples/config.yaml" "$BUILD_DIR/config.yaml"
  cp -R "$ROOT_DIR/skills/agentmux" "$BUILD_DIR/skill-agentmux"
  cp "$ROOT_DIR/README.md" "$BUILD_DIR/README.md"

  tar -C "$DIST_DIR" -czf "$DIST_DIR/${APP_NAME}_${VERSION}_${GOOS}_${GOARCH}.tar.gz" \
    "${APP_NAME}_${VERSION}_${GOOS}_${GOARCH}"
  rm -rf "$BUILD_DIR"
done

checksum_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
    return
  fi
  echo "missing sha256sum or shasum" >&2
  exit 1
}

(
  cd "$DIST_DIR"
  checksum_cmd ./*.tar.gz > checksums.txt
)

echo "Release artifacts written to $DIST_DIR"
