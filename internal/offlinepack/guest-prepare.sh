#!/usr/bin/env bash
# The full package's installation data was unpacked in /var/cache/cage-offline.
# Each file goes where an online installation leaves its download, so the
# installation runs as usual and finds it there: the packages in APT's
# download cache, the tool releases in cage_install_tool's cache. Until the
# installation finishes, APT reads only the indexes copied here: Debian's and
# NodeSource's own signed files. Nothing is trusted that the archives did not
# sign; apt-get checks every cached package against them.
set -euo pipefail
root=/var/cache/cage-offline
test -s "$root/apt/sources.list"
sudo install -d -m 0755 "$root/apt/sources.list.d"
printf '%s\n' \
  "Dir::Etc::sourcelist \"$root/apt/sources.list\";" \
  "Dir::Etc::sourceparts \"$root/apt/sources.list.d\";" \
  'Acquire::Languages "none";' |
  sudo tee /etc/apt/apt.conf.d/00cage-offline >/dev/null
if [ -d "$root/apt/archives" ]; then
  sudo install -d -m 0755 /var/cache/apt/archives
  sudo find "$root/apt/archives" -maxdepth 1 -type f -name '*.deb' -exec mv -f -t /var/cache/apt/archives/ {} +
fi
for directory in "$root"/tools/*/; do
  [ -d "$directory" ] || continue
  sum=$(basename "$directory")
  mkdir -p "$HOME/.cache/tool-downloads/$sum"
  for file in "$directory"*; do
    [ -f "$file" ] || continue
    target="$HOME/.cache/tool-downloads/$sum/$(basename "$file")"
    if [ ! -e "$target" ]; then
      sudo mv "$file" "$target"
      sudo chown "$(id -u):$(id -g)" "$target"
    fi
  done
done
printf 'Installation files are ready: %s packages\n' "$(find /var/cache/apt/archives -maxdepth 1 -name '*.deb' | wc -l)"
