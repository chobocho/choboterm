#!/bin/sh
# Installs choboterm for the current user: ~/.local/bin, the icon and a menu entry.
set -e
cd "$(dirname "$0")"
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/256x256/apps"
install -m 755 choboterm "$HOME/.local/bin/choboterm"
install -m 644 choboterm.png "$HOME/.local/share/icons/hicolor/256x256/apps/choboterm.png"
sed "s|^Exec=.*|Exec=$HOME/.local/bin/choboterm|" choboterm.desktop > "$HOME/.local/share/applications/choboterm.desktop"
echo "installed: $HOME/.local/bin/choboterm (menu: choboterm)"
