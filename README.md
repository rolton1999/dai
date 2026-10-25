# DAI

[![Go checks](https://github.com/rolton1999/dai/actions/workflows/lint.yml/badge.svg)](https://github.com/rolton1999/dai/actions/workflows/lint.yml)

A fast, well-tested Rutube downloader written in Go. Save videos and playlists
as MP4 at the highest supported resolution, without re-encoding.

- **Parallel:** 4 active videos, 8 segment workers per video by default.
- **Resumable:** completed MP4s are verified by size and SHA-256.
- **Safe:** collision checks, FFprobe validation, and atomic publication.
- **Observable:** individual progress rows and conversion spinners.

## Install

Requires Go 1.25+, FFmpeg, and FFprobe on `PATH`.
Supports macOS, Linux, DragonFly BSD, FreeBSD, NetBSD, and OpenBSD.

```sh
git clone https://github.com/rolton1999/dai.git
cd dai
make build
```

## Usage

```sh
./bin/dai https://rutube.ru/video/dace2feac379872d42e49b6b25c0587a/
./bin/dai --resume --jobs 4 --output ./videos https://rutube.ru/plst/1840659/
```

Flags must precede the URL.

| Flag                   | Description                                        |
| ---------------------- | -------------------------------------------------- |
| `--output <directory>` | Destination directory; default: current directory. |
| `--jobs <count>`       | Concurrent playlist videos; default: 4.            |
| `--resume`             | Verify and reuse completed playlist videos.        |
| `--overwrite`          | Replace existing output files.                     |
| `-h`, `--help`         | Show usage and flags.                              |

To resume, repeat the same command with the same output directory. Use `--resume`
from the first run; unfinished videos restart from the beginning.
`--resume` is playlist-only and cannot be combined with `--overwrite`.

Supports complete, unencrypted MPEG-TS HLS playlists. Existing files are preserved
unless `--overwrite` is set. See [download behavior](docs/downloads.md) for details.

## Development

```sh
make build
make lint
make test                   # Race-enabled; no live Rutube or FFmpeg required
make test-integration       # Local HTTP fixtures and real FFmpeg/FFprobe
make e2e-single             # Live Rutube video and reference checksum
make e2e-playlist           # Live playlist, one video worker
make e2e-playlist-parallel  # Live playlist, four video workers
make e2e-playlist-resume    # Live playlist recovery across two runs
```

`make e2e` runs all four live tests. They require network access, FFmpeg, FFprobe,
and `shasum`, and use isolated temporary directories. Outputs must match the
committed reference checksums; a different FFmpeg build or upstream re-encoding
can change them. See [E2E requirements and checksums](tests/e2e/README.md).

CI runs lint and race-enabled unit tests. Live Rutube checksum tests are opt-in
and have not been rerun against the current implementation.

[Testing and development](docs/development.md) · [Concurrency design](docs/concurrency.md)
