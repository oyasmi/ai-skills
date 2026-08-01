#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

mkdir -p "$BIN_DIR"

export GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
export GOPATH="${GOPATH:-$ROOT_DIR/.cache/go-path}"
export GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}"
mkdir -p "$GOCACHE" "$GOPATH" "$GOMODCACHE"

VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

echo "Building aiquota $VERSION..."
go build -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
  -o "$BIN_DIR/aiquota" "$ROOT_DIR/cmd/aiquota"

echo "Installed binary to $BIN_DIR/aiquota"

RESOLVED="$(command -v aiquota || true)"
if [[ -z "$RESOLVED" ]]; then
  echo
  echo "WARNING: $BIN_DIR is not on PATH, so a plain \`aiquota\` command will not be found."
elif [[ "$RESOLVED" != "$BIN_DIR/aiquota" ]]; then
  echo
  echo "WARNING: a plain \`aiquota\` resolves to $RESOLVED, not the copy just installed at $BIN_DIR/aiquota."
fi

echo
echo "Config file is shared with QuotaList: ~/.config/quota-list/config.json"
echo "Run: aiquota --json"
