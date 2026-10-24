#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_directory="$(dirname "$script_directory")"
checksum_file="$script_directory/checksums.sha256"
single_url='https://rutube.ru/video/dace2feac379872d42e49b6b25c0587a/'
playlist_url='https://rutube.ru/plst/1840659/'

selection="${1:-}"
case "$selection" in
    single) download_url="$single_url" ;;
    playlist) download_url="$playlist_url" ;;
    *) echo "Usage: $0 single|playlist" >&2; exit 2 ;;
esac

if ! grep -Eq "^[[:xdigit:]]{64}  $selection/" "$checksum_file"; then
    echo "Add manually calculated $selection checksums to $checksum_file first." >&2
    exit 1
fi

workspace="$(mktemp -d "${TMPDIR:-/tmp}/dai-e2e.XXXXXX")"
trap 'rm -rf "$workspace"' EXIT
mkdir "$workspace/$selection" "$workspace/segments"
export TMPDIR="$workspace/segments"

printf 'Downloading %s...\n' "$selection"
(cd "$workspace/$selection" && "$project_directory/bin/dai" "$download_url")

printf '\nChecking %s checksums...\n' "$selection"
cd "$workspace"
grep -E "^[[:xdigit:]]{64}  $selection/" "$checksum_file" > selected.sha256
awk '{ print substr($0, 67) }' selected.sha256 | LC_ALL=C sort > expected.files
find "$selection" -type f -name '*.mp4' | LC_ALL=C sort > actual.files
diff -u expected.files actual.files
shasum -a 256 -c selected.sha256
