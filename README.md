# DAI

A Go command-line downloader for Rutube videos and playlists. Selects the highest
resolution HLS variant and saves it as MP4 using FFmpeg, without re-encoding.

## Usage

Requires Go 1.25 or later to build, and `ffmpeg` and `ffprobe` on `PATH` to run.

```sh
make build
./bin/dai https://rutube.ru/video/dace2feac379872d42e49b6b25c0587a/
./bin/dai --output ./videos https://rutube.ru/plst/1840659/
```

Flags must precede the URL:

| Flag | Description |
| --- | --- |
| `--output <directory>` | Destination directory; defaults to the current directory. |
| `--overwrite` | Allow replacing existing output files. |

Accepts HTTPS links on `rutube.ru` with `/video/<32 hex ID>` or
`/plst/<positive numeric ID>`. A trailing slash, query, or fragment is allowed.

## Download behavior

Uses video titles as filenames, replacing separators and control characters with
underscores, trimming surrounding dots and spaces, and prefixing leading hyphens
with an underscore. Names are limited to 200 UTF-8 bytes before `.mp4`;
empty names become `video.mp4`.

Existing destinations require `--overwrite`. Duplicate playlist filenames are
rejected before downloading, even with overwrite enabled. Each MP4 is validated
with FFprobe before atomic publication. Failures remove operation-owned temporary
files and preserve existing destinations; completed playlist videos remain
available if a later download fails. Network retries have bounded deadlines;
Ctrl-C cancels active work.

Supports complete, unencrypted MPEG-TS HLS playlists in segment order. Resolution
determines variant selection; bandwidth breaks ties. Relative URLs resolve after
redirects. Separate media tracks, encryption, byte ranges, initialization maps,
and live or partial playlists are rejected.

## Development

```sh
make build
make lint              # Pinned linter; applies fixes, including integration sources
make test              # Race-enabled tests; no Rutube or FFmpeg required
make test-integration  # Local HTTP fixtures and real FFmpeg/FFprobe
```

Tests cover parsing, retries, cancellation, worker shutdown, output collisions,
concurrent publication, and cleanup, with fuzz targets for both HLS parsers.
Integration tests cover redirects, segment downloads, conversion, MP4 validation,
unusual filenames, and failure reporting. CI runs lint, unit, and integration checks.

Live checksum tests are opt-in: `make e2e-single` and `make e2e-playlist`.
`make test-all` runs all suites, stopping on failure. See
[live test requirements and reference checksums](tests/e2e/README.md).
Local fixtures do not establish live Rutube compatibility; live checksum tests
have not been rerun against the current implementation.
