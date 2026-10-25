# DAI

Download a Rutube video or playlist to MP4. Requires Go and FFmpeg/FFprobe.

```sh
make build
./bin/dai https://rutube.ru/video/dace2feac379872d42e49b6b25c0587a/
./bin/dai --output ./videos https://rutube.ru/plst/1840659/
```

Flags must precede the URL. `--output` defaults to the current directory.
`--overwrite` explicitly allows replacing existing output files. Existing files
otherwise cause an error before segment downloads start. Duplicate playlist
filenames are rejected before any video downloads start, including with overwrite.
A playlist publishes completed videos individually; an error preserves earlier
completed videos.

Accepted links use HTTPS and the exact `rutube.ru` host, with `/video/<32 hex ID>`
or `/plst/<positive numeric ID>`, optionally followed by a slash, query or fragment.

Titles become filenames: separators and control characters become underscores,
leading/trailing dots and spaces are trimmed, leading hyphens are prefixed, and
names are limited to 200 UTF-8 bytes before `.mp4`. Empty names become `video.mp4`.
Outputs stay in the selected directory. Conversion uses private temporary files,
validates the MP4 using FFprobe, and publishes it atomically. Failed operations
remove their own temporary data and preserve existing destinations.

HLS supports finite, unencrypted MPEG-TS playlists with segments in playlist order.
Master attributes may be reordered and quoted; relative URLs use the final URL
after redirects. The highest resolution is selected, with bandwidth breaking ties.
Separate audio/video tracks, encryption, byte ranges, initialization maps and live
or partial playlists are rejected explicitly. HTTP requests, retries, individual
segments and conversion have bounded deadlines; Ctrl-C cancels active work.

```sh
make lint
make test
make test-integration
make test-all
```

`make test-all` runs unit, integration, then both live E2E tests, stopping on the
first failure. It requires network access, FFmpeg/FFprobe and `shasum`.

The integration tests generate local media and use FFmpeg/FFprobe without
accessing Rutube. Ordinary tests use process doubles and need neither executable.
Live checksum tests are opt-in: `make e2e-single` and `make e2e-playlist` use
temporary directories. See [tests/e2e/README.md](tests/e2e/README.md) for reference
checksums.

The integration suite covers local HTTP metadata, master/media playlist redirects,
segment downloading, real FFmpeg conversion and FFprobe output validation. It also
checks quoted segment paths, colon/leading-hyphen output names, failure reporting
and cleanup. CI runs this suite with FFmpeg installed. Ordinary race-enabled tests
cover parsing, retry policy, cancellation, worker shutdown, output collisions,
concurrent publication and resource ownership; both HLS parsers have fuzz targets.

Local fixtures do not establish compatibility with the live Rutube service. The
live checksum tests have not been rerun on the assembled fixes.

`make lint` also checks integration-tagged sources and applies fixes. CI rejects
tracked source changes produced by lint. `make clean` removes only `bin/dai`.
The legacy `make test-small` and `make test-big` targets alias `e2e-single` and
`e2e-playlist`, respectively, and use the same isolated checksum workflow.

Metadata and HLS playlist requests make at most five attempts for transient
failures, with 1/2/3/4-second backoff and a five-minute operation deadline.
Permanent failures stop immediately. Segment transfers retain four attempts,
250/500/1000-millisecond backoff, and a two-minute operation deadline.
