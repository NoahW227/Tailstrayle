# Tailstrayle

A lightweight system tray app for Tailscale on Linux. Designed for KDE Plasma; should also work on other desktops that support the StatusNotifierItem protocol.

Built as a low-memory alternative to [Trayscale](https://github.com/DeedleFake/trayscale) - typical RSS is around 20–30 MB instead of 150+ MB, at the cost of a simpler feature set.

## Features

- Tray icon showing connected / disconnected state
- Left-click to toggle connect/disconnect
- Right-click menu with:
  - Current account name
  - Connect / Disconnect
  - Exit node selection
  - Copy this device's Tailscale IP
  - Manual refresh
- Auto-updates every 3 seconds - stays in sync if you use the CLI

## Requirements

- Linux with a StatusNotifierItem-compatible tray (KDE Plasma, XFCE, Cinnamon, or GNOME with the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/))
- [Tailscale](https://tailscale.com/download/linux) installed and running
- Go 1.21 or later (only needed to build from source)
- For Copy IP: `wl-clipboard` (Wayland) or `xclip` (X11)

### Fedora dependencies

```bash
sudo dnf install golang gtk3-devel libayatana-appindicator-gtk3-devel wl-clipboard
```

## Install

### From source

```bash
git clone https://github.com/NoahW227/Tailstrayle.git
cd Tailstrayle
./install.sh
```

The script builds the binary, installs it to `~/.local/bin/Tailstrayle`, and sets up autostart for KDE/GNOME.

### Manual install

```bash
go build -o Tailstrayle
mkdir -p ~/.local/bin
mv Tailstrayle ~/.local/bin/

# Autostart (optional)
mkdir -p ~/.config/autostart
cp Tailstrayle.desktop ~/.config/autostart/
sed -i "s|Exec=Tailstrayle|Exec=$HOME/.local/bin/Tailstrayle|" ~/.config/autostart/Tailstrayle.desktop
```

Make sure `~/.local/bin` is in your `$PATH`.

## First-time Tailscale setup

Tailstrayle talks to the Tailscale daemon as your normal user. You need to do two things once:

```bash
# Allow your user to control tailscaled
sudo tailscale set --operator=$(whoami)

# Log in (this still needs root)
sudo tailscale login
```

After this, Tailstrayle handles connect/disconnect/exit-nodes without prompting for a password.

If you use subnet routes, enable them once too:

```bash
tailscale set --accept-routes
```

## Usage

- **Left-click** the tray icon: toggle connect/disconnect
- **Right-click**: full menu

The account label at the top of the menu shows who you're logged in as. If you see "Not logged in", run `sudo tailscale login` from a terminal.

## Uninstall

```bash
rm ~/.local/bin/Tailstrayle
rm ~/.config/autostart/Tailstrayle.desktop
```

## Troubleshooting

**Icon doesn't appear (GNOME)**
Install and enable the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/), then log out and back in.

**"Access denied: prefs write access denied"**
The operator setting isn't applied. Run:
```bash
sudo tailscale set --operator=$(whoami)
sudo tailscale debug prefs | grep -i operator
```
Confirm `OperatorUser` shows your username. If you ever run `tailscale logout`, you'll need to set the operator again.

**"Some peers are advertising routes but --accept-routes is false"**
Run once: `tailscale set --accept-routes`

**Stale exit-node entries in the menu**
The menu pre-allocates 5 exit-node slots at startup and reuses them. If your tailnet has more than 5 exit-node-capable peers, increase `maxExitNodes` in `main.go`. Unused slots may render as blank rows on some desktops.

## Building for development

```bash
go build -o Tailstrayle
./Tailstrayle
```

Logs go to stderr.

## License

MIT - see [LICENSE](LICENSE).
