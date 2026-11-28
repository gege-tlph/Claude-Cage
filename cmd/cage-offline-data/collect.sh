#!/usr/bin/env bash
# Run as the work account in an environment that has just been installed
# online by the same build. Writes, as a tar stream on stdout, what that
# installation downloaded: APT's signed indexes and archives, the tool
# releases, the NodeSource key and code-server's schemas. Nothing personal:
# no home directory, account, proxy or log is read.
set -euo pipefail
stage=/var/tmp/cage-offline-collect
sudo rm -rf "$stage"
sudo install -d -m 0755 "$stage" "$stage/lists" "$stage/archives" "$stage/tools" "$stage/keys" "$stage/schemas"
trap 'sudo rm -rf "$stage"' EXIT
# Hard links: the same file system, no second copy.
sudo find /var/lib/apt/lists -maxdepth 1 -type f \( -name '*_InRelease' -o -name '*_Packages' -o -name '*_Packages.*' \) -exec cp -l {} "$stage/lists/" \;
for archives in /var/cache/apt/archives /var/cache/apt/install-archives; do
  [ -d "$archives" ] || continue
  sudo find "$archives" -maxdepth 1 -type f -name '*.deb' -exec cp -l {} "$stage/archives/" \;
done
for directory in "$HOME"/.cache/tool-downloads/*/; do
  [ -d "$directory" ] || continue
  sum=$(basename "$directory")
  for file in "$directory"mise.tar.gz "$directory"gh.deb "$directory"code-server.deb; do
    [ -f "$file" ] || continue
    sudo install -d -m 0755 "$stage/tools/$sum"
    sudo cp -l "$file" "$stage/tools/$sum/"
  done
done
if [ -f /etc/apt/keyrings/nodesource.asc ]; then sudo cp /etc/apt/keyrings/nodesource.asc "$stage/keys/"; fi
if [ -f "$HOME/.local/share/code-server/Machine/settings.json" ]; then
  sudo cp "$HOME/.local/share/code-server/Machine/settings.json" "$stage/schemas/code-server-machine-settings.json"
fi
dpkg-query -W -f='${binary:Package}\t${Version}\n' | sudo tee "$stage/dpkg.txt" >/dev/null
sudo cp /etc/os-release "$stage/os-release"
dpkg --print-architecture | sudo tee "$stage/arch" >/dev/null
sudo tar -c -C "$stage" .
