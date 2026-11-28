#!/usr/bin/env bash
# The full package's installation data was unpacked in /var/cache/cage-offline.
# Until the installation finishes, APT reads only the repositories copied
# there: Debian's and NodeSource's own signed indexes and the packages this
# installation uses. Nothing is trusted that the archives did not sign.
set -euo pipefail
root=/var/cache/cage-offline
test -s "$root/apt/sources.list"
sudo install -d -m 0755 "$root/apt/sources.list.d"
printf '%s\n' \
  "Dir::Etc::sourcelist \"$root/apt/sources.list\";" \
  "Dir::Etc::sourceparts \"$root/apt/sources.list.d\";" \
  'Acquire::Languages "none";' |
  sudo tee /etc/apt/apt.conf.d/00cage-offline >/dev/null
# The tool releases go where cage_install_tool finds a verified copy first.
for directory in "$root"/tools/*/; do
  [ -d "$directory" ] || continue
  sum=$(basename "$directory")
  mkdir -p "$HOME/.cache/tool-downloads/$sum"
  for file in "$directory"*; do
    [ -f "$file" ] || continue
    target="$HOME/.cache/tool-downloads/$sum/$(basename "$file")"
    [ -e "$target" ] || cp "$file" "$target"
  done
done
printf 'Installation files are ready: %s\n' "$(du -sh "$root" | cut -f1)"
