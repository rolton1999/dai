# Live E2E tests

Run `make e2e-single` or `make e2e-playlist` from the project root.
`make e2e` runs both; `make e2e-singe` is an alias for `make e2e-single`.
Each target streams download/conversion progress and checksum results directly
to the terminal. It builds `cmd/dai`, downloads the single
video and the complete playlist linked in `run.sh`, and checks every MP4 against
`checksums.sha256`. Missing, extra, or changed files fail the test.

Requires network access, FFmpeg, and `shasum`. Downloads and segments live in a
temporary directory that is removed when the script exits. Existing user files
are untouched.

Calculate the reference checksums manually from verified downloads, arranged in
`single/` and `playlist/` directories:

```sh
cd /path/to/verified-downloads
shasum -a 256 single/*.mp4 playlist/*.mp4 > /path/to/dai/e2e/checksums.sha256
```

The script never generates or updates reference checksums. Use the same FFmpeg
build as the reference: different muxer versions or upstream re-encoding can
change full-file checksums. An empty reference file fails before downloading.

The initial references use FFmpeg 9.0.2. During manual reference capture, the output
path was made absolute for the playlist title beginning with `6:57`. The current
CLI passes a relative path, which FFmpeg interprets as a protocol. The E2E script
does not work around this application bug: it reports the conversion failure.
