#!/usr/bin/env bash
# The end of a Cage setup. After an installation from the full package, APT
# returns to the system's own sources, and the copied indexes (each source's
# own signed files) become those sources' lists, as an online installation
# leaves them. The packages and tool releases already sit where an online
# installation keeps its downloads. The marker tells later boots (and every
# environment cloned from this disk) that the installation is complete.
set -euo pipefail
root=/var/cache/cage-offline
lists=/var/lib/apt/lists
if [ -e /etc/apt/apt.conf.d/00cage-offline ] || [ -d "$root" ]; then
  sudo rm -f /etc/apt/apt.conf.d/00cage-offline
  if [ -s "$root/apt/lists.map" ]; then
    while IFS=$'\t' read -r repo prefix; do
      [ -n "$repo" ] && [ -n "$prefix" ] && [ -d "$root/apt/$repo/dists" ] || continue
      while IFS= read -r -d '' file; do
        relative=${file#"$root/apt/$repo/dists/"}
        sudo mv -f "$file" "$lists/${prefix}_dists_${relative//\//_}"
      done < <(find "$root/apt/$repo/dists" -type f -print0)
    done < "$root/apt/lists.map"
  fi
  # APT's own entries for the local repositories name a folder that goes.
  sudo find "$lists" -maxdepth 1 -type f -name '_var_cache_cage-offline_*' -delete
  sudo rm -rf "$root"
  echo 'Installation files are in place.'
fi
sudo install -d -m 0755 /etc/workbench
sudo touch /etc/workbench/offline-install-finished
