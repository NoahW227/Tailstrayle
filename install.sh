#!/usr/bin/env bash
set -euo pipefail

BIN_DIR="$HOME/.local/bin"
AUTOSTART_DIR="$HOME/.config/autostart"
BIN_PATH="$BIN_DIR/Tailstrayle"

echo "Building Tailstrayle..."
go build -o Tailstrayle .

echo "Installing to $BIN_PATH..."
mkdir -p "$BIN_DIR"
mv Tailstrayle "$BIN_PATH"

echo "Setting up autostart..."
mkdir -p "$AUTOSTART_DIR"
sed "s|Exec=Tailstrayle|Exec=$BIN_PATH|" Tailstrayle.desktop > "$AUTOSTART_DIR/Tailstrayle.desktop"

echo
echo "Done. Tailstrayle is installed at $BIN_PATH"
echo "It will start automatically on next login, or run it now:"
echo "  $BIN_PATH"
echo
if ! command -v tailscale >/dev/null 2>&1; then
  echo "Warning: 'tailscale' command not found. Install Tailscale first:"
  echo "  https://tailscale.com/download/linux"
fi
if ! sudo -n tailscale debug prefs 2>/dev/null | grep -q "\"OperatorUser\": \"$(whoami)\""; then
  echo "Reminder: run this once so Tailstrayle can control Tailscale without sudo:"
  echo "  sudo tailscale set --operator=\$(whoami)"
fi
