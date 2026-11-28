#!/usr/bin/env bash
# Builds the full package's installation data on an x86_64 Linux host with
# KVM. A throwaway Cage profile installs the environment online, through the
# proxy node EXIT (host:port, not an address of this host), the way a user's
# first start does; cage-offline-data then collects what that installation
# downloaded and assembles OUT. Only the agent interface drives the app
# (docs/guide/agent.md), so no window is needed.
#
# Usage: scripts/build-offline-data.sh CAGE TOOL EXIT WORK OUT
#   CAGE  the cage binary of this build (mihomo next to it)
#   TOOL  cage-offline-data of this build
#   EXIT  host:port of a SOCKS5 proxy for the installation
#   WORK  empty directory for the profile and the collected files
#   OUT   directory to write the data to (must not exist)
set -euo pipefail
[[ $# == 5 ]] || { sed -n '2,15p' "$0" >&2; exit 2; }
cage=$(realpath "$1") tool=$(realpath "$2") exit_node=$3 work=$4 out=$5
cd "$(dirname "$0")/.."
version=$(tr -d '\r\n' < VERSION)
mkdir -p "$work"
work=$(realpath "$work")
[[ ! -e "$out" ]] || { echo "$out exists" >&2; exit 1; }
config="$work/profile/config.json"
export CLAUDE_CAGE_CONFIG="$config"

agent() { "$cage" agent "$@" --config "$config"; }
# Prints a call's reply without the data an account creation returns once.
call() {
  local name=$1
  shift
  "$cage" agent call "$name" --config "$config" "$@" | tee "$work/last-reply.json" | jq -c 'del(.data.recovery_key?) | {status, error, data_keys: (.data | if type == "object" then keys else type end)}'
}

echo '== profile and service'
agent init
agent service.start

echo '== account (random password, never printed)'
umask 077
password=$(head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)
jq -n --arg p "$password" '{username: "builder", password: $p, confirm_password: $p}' > "$work/account.json"
call account.create --input "$work/account.json"
rm -f "$work/account.json" "$work/last-reply.json"
umask 022

echo '== proxy node'
call initialization.set --input - <<<'{"action":"begin"}'
host=${exit_node%:*} port=${exit_node##*:}
yaml=$(printf 'proxies:\n  - name: exit\n    type: socks5\n    server: %s\n    port: %s\n    udp: false\n' "$host" "$port")
jq -n --arg c "$yaml" '{name: "build", content: $c}' | call proxy.import --input - --wait 5m
"$cage" agent call proxy.status --config "$config" > "$work/proxy.json"
node=$(jq -r '[.. | objects | select(.name? == "exit" and (.id? | type) == "string")][0].id' "$work/proxy.json")
[[ -n "$node" && "$node" != null ]] || { echo 'The imported node has no id' >&2; exit 1; }
jq -n --arg id "$node" '{node_id: $id}' | call proxy.select --input - --wait 5m

echo '== install (official Debian source)'
"$cage" agent request POST /api/host/debian-mirrors --config "$config" --input - <<<'{"preference":"official"}' | jq -c '{status, error}' || true
call initialization.set --input - <<<'{"action":"install"}'
started=$(date +%s)
call environment.start --input - --wait 60m <<<'{"guest_system":"debian-13"}'
echo "installed in $(( $(date +%s) - started )) s"
"$cage" agent call status --config "$config" | jq -c '.data | {ready, vm_running, network_open}'

echo '== collect and assemble'
"$tool" collect -cage "$cage" -config "$config" -out "$work/collected.tar"
ls -l "$work/collected.tar"
# Lima's download cache: CAGE_LIMA_CACHE_DIR, the user cache, or the profile's storage.
data=
for dir in "${CAGE_LIMA_CACHE_DIR:-}" "$HOME/.cache/lima" "$work"; do
  [[ -n "$dir" && -d "$dir" ]] || continue
  data=$(find "$dir" -path '*/download/by-url-sha256/*/data' -print -quit 2>/dev/null || true)
  [[ -z "$data" ]] || break
done
[[ -n "$data" ]] || { echo 'The Debian image is not in the Lima cache' >&2; exit 1; }
echo "image: $(dirname "$data")"
"$tool" assemble -collected "$work/collected.tar" -image-entry "$(dirname "$data")" -version "$version" \
  -tool-versions internal/cage/tool-versions.env -out "$out"
call environment.stop --wait 10m || true
