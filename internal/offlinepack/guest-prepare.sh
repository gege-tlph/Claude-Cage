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
# Lima installs its own dependencies at every boot (rsync; sshfs for
# reverse-sshfs shares). At this first boot APT had no packages yet, so that
# step failed; it runs again now with Lima's settings, so the system gets what
# an online installation's first boot gives it. As at boot, a failure is a
# warning.
sudo sh -eu -c '
  [ -f "$1/boot.Linux/30-install-packages.sh" ] && [ -f "$1/lima.env" ] || exit 0
  while read -r line; do [ -n "$line" ] && export "$line"; done < "$1/lima.env"
  if [ -f "$1/param.env" ]; then
    while read -r line; do [ -n "$line" ] && export "$line"; done < "$1/param.env"
  fi
  export LIMA_CIDATA_MNT="$1"
  sh "$1/boot.Linux/30-install-packages.sh"
' sh /mnt/lima-cidata || echo 'WARNING: Lima dependency installation failed.' >&2
printf 'Installation files are ready: %s packages\n' "$(find /var/cache/apt/archives -maxdepth 1 -name '*.deb' | wc -l)"
