# Development

[Back to the README](../README.md)

The test suite covers parsing, retries, cancellation, worker limits, immediate
slot reuse, shutdown, output collisions, concurrent publication, progress rows,
playlist recovery, quoted remote titles in errors, and exclusive-rename fallback
publication. Local FFmpeg fixtures also test a truncated, packet-aligned segment
served with a successful HTTP response. Both HLS parsers have fuzz targets.
Dependencies are injected so failure paths can run against local HTTP fixtures and process doubles.

```sh
make build
make lint              # Pinned linter; applies fixes, including integration sources
make test              # Race-enabled tests; no live Rutube or FFmpeg required
make test-integration  # Local HTTP fixtures and real FFmpeg/FFprobe
```

| Layer                      | What it verifies                                                                                                                                                                                      | Entry points                                                                                                                                                                     |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Unit and concurrency tests | Cancellation, ordering, bounded work, cleanup, retry policies, output preservation, manifest validation, checksums, stable filenames, checkpoint failures, and lock release after process termination | [`internal/pipeline`](../internal/pipeline), [`internal/hls`](../internal/hls), [`internal/download`](../internal/download), [`internal/progress`](../internal/progress)         |
| Local integration tests    | Redirects, real segment conversion, MP4 validation, unusual filenames, corrupt middle/final segments, failure reporting, and reuse of completed MP4s                                                  | [Download](../internal/download/local_integration_test.go), [recovery](../internal/download/resume_integration_test.go), [FFmpeg](../internal/ffmpeg/concat_integration_test.go) |
| Live checksum smoke tests  | Known video and playlist outputs, sequential/parallel downloads, and restart recovery against Rutube                                                                                                  | [E2E requirements and checksums](../tests/e2e/README.md)                                                                                                                         |

[CI](../.github/workflows/lint.yml) runs lint and race-enabled unit tests. Integration
tests run locally on demand. Concurrency tests wait for observable conditions
with deadlines. Retry tests use fake time and injected waits to avoid wall-clock
backoff delays.

Live tests are opt-in: `make e2e-single`, `make e2e-playlist` (one video worker),
`make e2e-playlist-parallel` (four workers), and `make e2e-playlist-resume`
(restart recovery with four workers). `make test-small` and `make test-big` alias
the single-video and sequential-playlist tests. `make test-all` runs every suite,
including live tests, and stops on failure.

Local fixtures do not establish live Rutube compatibility. Live checksum tests
have not been rerun against the current implementation.
