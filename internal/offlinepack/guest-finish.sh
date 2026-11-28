#!/usr/bin/env bash
# The end of a Cage setup. After an offline installation, APT returns to the
# system's own sources and the copied data goes; its indexes would name files
# that no longer exist. The marker tells later boots (and every environment
# cloned from this disk) that the installation is complete.
set -euo pipefail
root=/var/cache/cage-offline
if [ -e /etc/apt/apt.conf.d/00cage-offline ] || [ -d "$root" ]; then
  sudo rm -f /etc/apt/apt.conf.d/00cage-offline
  sudo find /var/lib/apt/lists -maxdepth 1 -type f -name '_var_cache_cage-offline_*' -delete
  sudo rm -rf "$root"
  # The disk discards freed blocks, so clones do not copy the removed data.
  sudo fstrim / >/dev/null 2>&1 || true
  echo 'Installation files removed.'
fi
sudo install -d -m 0755 /etc/workbench
sudo touch /etc/workbench/offline-install-finished
