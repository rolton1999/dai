#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_directory="$(cd "$script_directory/../.." && pwd)"
checksum_file="$script_directory/checksums.sha256"
single_url='https://rutube.ru/video/dace2feac379872d42e49b6b25c0587a/'
playlist_url='https://rutube.ru/plst/1840659/'

sequential_jobs=1
parallel_jobs=4

selection="${1:-}"
checksum_selection="$selection"
jobs="$sequential_jobs"
case "$selection" in
    single) download_url="$single_url" ;;
    playlist) download_url="$playlist_url" ;;
    playlist-parallel|playlist-resume)
        download_url="$playlist_url"
        checksum_selection="playlist"
        jobs="$parallel_jobs"
        ;;
    *) echo "Usage: $0 single|playlist|playlist-parallel|playlist-resume" >&2; exit 2 ;;
esac

download_flags=(--jobs "$jobs")
if [[ "$selection" == "playlist-resume" ]]; then
    download_flags+=(--resume)
fi

workspace=""
finish() {
    local status="$?"
    trap - EXIT
    if [[ -n "$workspace" ]]; then
        rm -rf "$workspace" || true
    fi
    if [[ "$status" -eq 0 ]]; then
        printf '\nE2E %s: PASSED\n' "$selection"
    else
        printf '\nE2E %s: FAILED\n' "$selection" >&2
    fi
    exit "$status"
}
trap finish EXIT

if ! grep -Eq "^[[:xdigit:]]{64}  $checksum_selection/" "$checksum_file"; then
    echo "Add manually calculated $selection checksums to $checksum_file first." >&2
    exit 1
fi

workspace="$(mktemp -d "${TMPDIR:-/tmp}/dai-e2e.XXXXXX")"
mkdir "$workspace/$checksum_selection" "$workspace/segments"
export TMPDIR="$workspace/segments"

download() {
    printf 'Downloading %s with --jobs %s...\n' "$selection" "$jobs"
    (cd "$workspace/$checksum_selection" && "$project_directory/bin/dai" "${download_flags[@]}" "$download_url")
}

check_outputs() (
    printf '\nChecking %s checksums...\n' "$selection"
    cd "$workspace"
    grep -E "^[[:xdigit:]]{64}  $checksum_selection/" "$checksum_file" > selected.sha256
    awk '{ print substr($0, 67) }' selected.sha256 | LC_ALL=C sort > expected.files
    find "$checksum_selection" -type f -name '*.mp4' | LC_ALL=C sort > actual.files
    diff -u expected.files actual.files
    shasum -a 256 -c selected.sha256
)

download
check_outputs

if [[ "$selection" == "playlist-resume" ]]; then
    recover_file=""
    IFS= read -r recover_file < "$workspace/actual.files"
    mkdir "$workspace/preserved"
    while IFS= read -r output; do
        if [[ "$output" != "$recover_file" ]]; then
            ln "$workspace/$output" "$workspace/preserved/${output#*/}"
        fi
    done < "$workspace/actual.files"

    printf '\nRemoving test-owned output before restart: %s\n' "$recover_file"
    rm "$workspace/$recover_file"
    printf 'Restarting with --resume...\n'
    download
    check_outputs

    while IFS= read -r output; do
        if [[ "$output" != "$recover_file" && ! "$workspace/$output" -ef "$workspace/preserved/${output#*/}" ]]; then
            printf 'Resume replaced completed output: %s\n' "$output" >&2
            exit 1
        fi
    done < "$workspace/actual.files"
fi
