# Live E2E tests

Run these targets from the project root:

| Target | Download |
| --- | --- |
| `make e2e-single` | Single video with `--jobs 1`. |
| `make e2e-playlist` | Sequential playlist with `--jobs 1`. |
| `make e2e-playlist-parallel` | Concurrent playlist with `--jobs 4`. |

`make e2e` runs all three. Both playlist modes verify the same reference checksums
in separate temporary directories. The concurrent test checks completed outputs;
unit tests independently verify the worker limit and immediate slot reuse.

Each target streams download/conversion progress and checksum results directly
to the terminal and finishes with `E2E single: PASSED` or `E2E playlist-parallel: FAILED`
(as applicable), preserving a nonzero exit status on failure.
It builds `cmd/dai`, downloads the single
video and the complete playlist linked in `run.sh`, and checks every MP4 against
`checksums.sha256`. Missing, extra, or changed files fail the test.

Requires network access, FFmpeg/FFprobe, and `shasum`. Downloads and segments live in a
temporary directory that is removed when the script exits. Existing user files
are untouched.

Calculate the reference checksums manually from verified downloads, arranged in
`single/` and `playlist/` directories:

```sh
cd /path/to/verified-downloads
shasum -a 256 single/*.mp4 playlist/*.mp4 > /path/to/dai/tests/e2e/checksums.sha256
```

The script never generates or updates reference checksums. Use the same FFmpeg
build as the reference: different muxer versions or upstream re-encoding can
change full-file checksums. An empty reference file fails before downloading.

The initial references use FFmpeg 9.0.2. During manual reference capture, the output
path was made absolute for the playlist title beginning with `6:57`. The CLI now also passes an absolute output path so FFmpeg does not interpret
`6:57` as a protocol. The E2E script itself contains no filename workaround.
