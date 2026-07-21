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

echo "Building agentmux..."
go build -o "$BIN_DIR/agentmux" "$ROOT_DIR/cmd/agentmux"

if [[ "$OVERWRITE_CONFIG" == "1" || ! -f "$CONFIG_FILE" ]]; then
  echo "Installing config to $CONFIG_FILE"
  cp "$ROOT_DIR/examples/config.yaml" "$CONFIG_FILE"
else
  echo "Keeping existing config at $CONFIG_FILE"
fi

echo "Installed binary to $BIN_DIR/agentmux"
echo
echo "Next steps:"
echo "  1. Ensure $BIN_DIR is in PATH"
echo "  2. Review $CONFIG_FILE"
echo "  3. Run: agentmux template list --json"
