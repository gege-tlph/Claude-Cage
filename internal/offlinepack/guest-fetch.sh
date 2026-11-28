
# A download that the full package's installation data holds, found by its
# URL (files/<SHA-256 of the URL>), is copied instead of fetched. The
# arguments are cage_curl_download's, as curl takes them.
cage_offline_fetch() {
  local root=/var/cache/cage-offline url='' out='' code=0 arg previous='' file
  [ -d "$root/files" ] || return 1
  for arg in "$@"; do
    case "$previous" in -o) out="$arg" ;; -w|--write-out) code=1 ;; esac
    case "$arg" in https://*) url="$arg" ;; esac
    previous="$arg"
  done
  [ -n "$url" ] && [ -n "$out" ] || return 1
  file="$root/files/$(printf '%s' "$url" | sha256sum | cut -c1-64)"
  [ -f "$file" ] || return 1
  cp "$file" "$out" || return 1
  printf 'Copied %s from the installation files.\n' "${url##*/}" >&2
  if [ "$code" = 1 ]; then printf 200; fi
}
