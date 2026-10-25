# DAI

A Go command-line downloader for Rutube videos and playlists. Selects the highest
resolution HLS variant and saves it as MP4 using FFmpeg, without re-encoding.

## Usage

Requires Go 1.25 or later to build, and `ffmpeg` and `ffprobe` on `PATH` to run.

```sh
make build
./bin/dai https://rutube.ru/video/dace2feac379872d42e49b6b25c0587a/
./bin/dai --resume --jobs 4 --output ./videos https://rutube.ru/plst/1840659/
```

Flags must precede the URL:

| Flag | Description |
| --- | --- |
| `--output <directory>` | Destination directory; defaults to the current directory. |
| `--jobs <count>` | Concurrent playlist videos; defaults to 4. Use 1 for sequential downloads. |
| `--resume` | Record playlist progress and reuse verified completed MP4s on restart. |
| `--overwrite` | Allow replacing existing output files. |

Accepts HTTPS links on `rutube.ru` with `/video/<32 hex ID>` or
`/plst/<positive numeric ID>`. A trailing slash, query, or fragment is allowed.

## Download behavior

Playlists keep up to four videos active by default, starting the next as soon as
one finishes conversion, validation, and cleanup. Each active video has its own
terminal progress row, with segment completion percentages and conversion
spinners. A fixed header shows playlist totals; rows stay in playlist order,
including completed and queued videos. Small terminals prioritize active videos
and recent completions. Redirected output uses completion lines in playlist order.
Each video can transfer eight segments concurrently (`--jobs 4` permits up to 32
segment transfers).

Uses video titles as filenames, replacing separators and control characters with
underscores, trimming surrounding dots and spaces, and prefixing leading hyphens
with an underscore. Names are limited to 200 UTF-8 bytes before `.mp4`;
empty names become `video.mp4`.

Existing destinations require `--overwrite`. Duplicate playlist filenames are
rejected before downloading, even with overwrite enabled. Collision checks use
the destination filesystem's case and Unicode normalization rules. Each MP4 is
validated with FFprobe before atomic publication. FFmpeg input and conversion
errors fail the operation, including corrupt segments after valid input.
Failures remove operation-owned temporary files and preserve existing
destinations; completed playlist videos remain
available if a later download fails. Network retries have bounded deadlines;
Ctrl-C or the first failure stops scheduling and cancels active work; all workers
finish cleanup before exit.

Use `--resume` from the first playlist run, then repeat the command with the same
output directory after interruption. DAI keeps a versioned
`.dai-playlist-<ID>.json` manifest and a `.lock` file there. It verifies recorded
MP4s by size and SHA-256, skips matches, and downloads missing or unfinished videos
from the beginning. Fragments are never reused. Recorded filenames remain stable
if titles change. Changed, untracked, or symlink outputs cause an error and are
preserved; move conflicting files aside to continue. Resume is playlist-only and
cannot combine with `--overwrite`. The lock prevents concurrent resume runs for
the same playlist and releases automatically on process exit. Keep the manifest
with the downloaded files; completed runs retain it for future reuse.

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

Tests cover parsing, retries, cancellation, worker shutdown, output collisions
under the host filesystem's case and Unicode normalization rules,
concurrent publication, playlist worker limits and slot reuse, cancellation
cleanup, independent progress rows, and playlist recovery after interruption.
Both HLS parsers have fuzz targets.
Recovery tests cover output checksums, stable filenames, invalid manifests,
generated filenames truncated at dots, spaces, and UTF-8 boundaries, checkpoint
failures, and lock release after process termination.
Integration tests cover redirects, segment downloads, conversion, MP4 validation,
unusual filenames, corrupt middle and final segments, and failure reporting.
CI runs lint and unit tests; integration tests run locally on demand.

Live checksum tests are opt-in: `make e2e-single`, `make e2e-playlist` (one job),
`make e2e-playlist-parallel` (four jobs), and `make e2e-playlist-resume`
(restart recovery with four jobs).
`make test-all` runs all suites, stopping on failure. See
[live test requirements and reference checksums](tests/e2e/README.md).
Local fixtures do not establish live Rutube compatibility; live checksum tests
have not been rerun against the current implementation. Windows resume behavior
has been cross-compiled but not tested at runtime.
