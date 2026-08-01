#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/agentmux"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
OVERWRITE_CONFIG="${OVERWRITE_CONFIG:-0}"

mkdir -p "$BIN_DIR"
mkdir -p "$CONFIG_DIR"

export GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
export GOPATH="${GOPATH:-$ROOT_DIR/.cache/go-path}"
export GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}"

mkdir -p "$GOCACHE" "$GOPATH" "$GOMODCACHE"

# A stamped version is what lets `agentmux doctor` and `agentmux version --json`
# tell a fresh build apart from a stale one instead of both reporting "dev".
VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

echo "Building agentmux $VERSION..."
go build -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
  -o "$BIN_DIR/agentmux" "$ROOT_DIR/cmd/agentmux"

if [[ "$OVERWRITE_CONFIG" == "1" || ! -f "$CONFIG_FILE" ]]; then
  echo "Installing config to $CONFIG_FILE"
  cp "$ROOT_DIR/examples/config.yaml" "$CONFIG_FILE"
else
  echo "Keeping existing config at $CONFIG_FILE"
fi

echo "Installed binary to $BIN_DIR/agentmux"

# A binary earlier on PATH silently shadows this one: every command still
# "works", just against stale behavior. Catch that here instead of leaving it
# for the next confused bug report.
RESOLVED="$(command -v agentmux || true)"
if [[ -z "$RESOLVED" ]]; then
  echo
  echo "WARNING: $BIN_DIR is not on PATH, so a plain \`agentmux\` command will not be found."
elif [[ "$RESOLVED" != "$BIN_DIR/agentmux" ]]; then
  echo
  echo "WARNING: a plain \`agentmux\` resolves to $RESOLVED, not the copy just installed at $BIN_DIR/agentmux."
  echo "         Put $BIN_DIR earlier in PATH, or remove the other copy, or the new build will not take effect."
fi

echo
echo "Next steps:"
echo "  1. Ensure $BIN_DIR is in PATH"
echo "  2. Review $CONFIG_FILE"
echo "  3. Run: agentmux doctor --json"
