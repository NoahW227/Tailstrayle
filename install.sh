#!/usr/bin/env bash
set -euo pipefail

BIN_DIR="$HOME/.local/bin"
AUTOSTART_DIR="$HOME/.config/autostart"
BIN_PATH="$BIN_DIR/Tailstrayle"

# Run from the repo directory so this works no matter where it's called from.
cd "$(dirname "$0")"

# Go installed from the official tarball often isn't on PATH yet.
if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
  export PATH="$PATH:/usr/local/go/bin"
fi

# Figure out which packages are missing. Clipboard tool depends on the session
# type and is only needed for "Copy IP", but it's small so we install it too.
missing=()
if ! command -v go >/dev/null 2>&1; then
  missing+=(go)
fi
if [ "${XDG_SESSION_TYPE:-}" = "x11" ]; then
  command -v xclip >/dev/null 2>&1 || missing+=(xclip)
else
  command -v wl-copy >/dev/null 2>&1 || missing+=(wl-clipboard)
fi

if [ ${#missing[@]} -gt 0 ]; then
  # Map generic names to this distro's package names.
  if command -v dnf >/dev/null 2>&1; then
    pm=(dnf install); go_pkg=golang
  elif command -v apt-get >/dev/null 2>&1; then
    pm=(apt-get install); go_pkg=golang-go
  elif command -v pacman >/dev/null 2>&1; then
    pm=(pacman -S --needed); go_pkg=go
  elif command -v zypper >/dev/null 2>&1; then
    pm=(zypper install); go_pkg=go
  else
    echo "Missing dependencies: ${missing[*]}"
    echo "Couldn't detect your package manager - please install them manually and re-run."
    exit 1
  fi

  pkgs=()
  for dep in "${missing[@]}"; do
    if [ "$dep" = go ]; then pkgs+=("$go_pkg"); else pkgs+=("$dep"); fi
  done

  echo "Missing dependencies: ${pkgs[*]}"
  if [ "$(id -u)" -eq 0 ]; then
    "${pm[@]}" "${pkgs[@]}"
  else
    echo "Installing them needs root, so you'll be asked for your password."
    sudo "${pm[@]}" "${pkgs[@]}"
  fi
else
  echo "All dependencies already installed."
fi

# Distro Go packages can be older than go.mod requires. Go 1.21+ can fetch the
# right toolchain itself (into the user's cache, no root needed), but some
# distros turn that off by default, so allow it unless the user chose otherwise.
export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"

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
elif ! tailscale debug prefs 2>/dev/null | grep -q "\"OperatorUser\": \"$(whoami)\""; then
  echo "Reminder: run this once so Tailstrayle can control Tailscale without sudo:"
  echo "  sudo tailscale set --operator=\$(whoami)"
fi
