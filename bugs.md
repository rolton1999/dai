# Bug fix plan

This plan covers the remaining bugs identified in the project review. Complete
the stages in order, keeping `make lint` and `make test` passing after each stage.

## 1. Parse and validate input URLs

- [x] Use `net/url` to validate the host and exact path structure.
- [x] Extract video and playlist IDs independently of queries and fragments.
- [x] Reject missing IDs and misleading paths such as `/videosomething/`.

Tests:

- Table-driven tests for video and playlist links with trailing slashes, queries,
  fragments, missing IDs, incorrect hosts, and invalid paths.
- Keep the existing no-argument regression test.

Implemented independently on `codex/fix-url-validation`. Accepted links use HTTPS,
the exact `rutube.ru` host (case-insensitive, without credentials or explicit
ports), and `/video/<32 hex ID>` or `/plst/<positive numeric ID>`. A single trailing
slash, query and fragment are allowed; query/fragment values do not enter the ID.
Malformed paths and missing IDs fail before metadata requests. Parser table tests
and the existing no-argument/empty-argument regressions cover this change. Other
stages below remain separate work.

## 2. Replace positional HLS parsing

- [x] Parse media playlist tags and URI lines explicitly instead of assuming fixed line offsets.
- [x] Replace positional parsing in master playlists.
- [x] Read variant attributes by name, respecting quoted commas.
- [x] Support LF, CRLF, blank lines, comments, and variable headers in media playlists.
- [x] Support variable master playlist tags and reordered attributes.
- [x] Resolve segment references against the final response URL after redirects,
  including relative, root-relative, and absolute references.
- [x] Resolve variant references against the final master playlist URL.
- [x] Return errors for empty, malformed, or unsupported playlists instead of
  panicking or producing incomplete output.
- [x] Make best-variant selection reject empty lists and invalid resolutions.
- [x] Explicitly reject unsupported encryption, byte ranges, and separate audio
  tracks until those features are implemented.

Tests:

- Table-driven master and media fixtures covering trailing newlines, variable
  headers, comments, CRLF, reordered or omitted optional attributes, and quoted
  codec lists.
- URL resolution tests, including redirects.
- Empty playlists, truncated entries, malformed attributes, invalid resolutions,
  and unsupported playlist features must produce clear errors.
- Fuzz both parsers: arbitrary input must never panic.

Master-playlist portion implemented independently on `codex/fix-hls-master-parsing`.
`internal/hls/master_playlist_test.go` covers optional/reordered attributes, quoted
codec lists, LF/CRLF, blank lines/comments, malformed/truncated entries, duplicate
attributes, invalid dimensions/rates, URL resolution after redirects, unsupported
external tracks/encryption and empty variant selection. The parser also has a
fuzz target; a five-second run passed over 311,000 inputs without a crash. Build,
lint and race-enabled tests passed. Variant selection ranks pixel area, then bandwidth for identical
resolutions. Master parsing rejects external media tracks and session encryption;
unsupported media-playlist features remain work for a separate branch.

The download caller now propagates selection errors. HTTP retry policy, bounded
reads, dependency injection, media parsing and download/conversion lifecycle are
unchanged by this fix. No new live-service compatibility claim is made here.

Media-playlist validation implemented independently on
`codex/fix-hls-media-validation`. Parsing requires ENDLIST and positive, finite
EXTINF durations. Encryption (except METHOD=NONE), byte ranges, initialization
maps, partial/delta streams and I-frame-only playlists are rejected explicitly.
Segment URLs must resolve to HTTP(S) without credentials. The existing Rutube
response-structure example is preserved.

`internal/hls/media_playlist_test.go` covers valid complete playlists, unsupported
features, incomplete streams, malformed durations and invalid URLs. A public
download regression verifies rejection before any segment request and cleanup of
the operation's temporary directory. Both parsers now have fuzz targets. The media parser passed a five-second fuzz
run covering over 305,000 inputs. Build, lint and race tests pass; live
Rutube compatibility was not rerun for this parsing-only change.

## 3. Bound HTTP requests and report failures explicitly

- [x] Introduce an injectable HTTP client with timeouts and context cancellation.
- [x] Check HTTP status before parsing metadata or playlists.
- [x] Validate required metadata fields instead of accepting error JSON as an
  empty successful response.
- [x] Close response bodies on every success and failure path, including master
  playlist responses.

Tests:

- Local HTTP servers or fake transports for 404/503 responses, malformed JSON,
  missing fields, truncated bodies, redirects, and stalled requests.
- Track response body closure on success and failure.
- Verify a missing playlist returns an error instead of an empty success.
- Verify timeouts and cancellation finish within bounded deadlines.

HTTP status handling implemented independently on `codex/fix-http-status-handling`.
Rutube video metadata, playlist titles, every playlist page and HLS master
playlists now require HTTP 200 before reading/parsing the response. Error bodies
are closed without being parsed; returned errors include the HTTP status.
Regression fixtures reproduce HTTP 404/503 being accepted as successful metadata
or playlists before the fix, including valid-looking bodies on error responses.
Tests also cover success, body closure, malformed success JSON and a missing
playlist through the public API. Required JSON field validation, injectable HTTP
readers and response limits are completed below. Retry classification remains
separate work.

Live checksum scripts and unchanged reference hashes now live in `tests/e2e`.
Makefile targets use the new path; the script resolves the project root two levels
above its location. The move is checked with shell syntax, Makefile dry runs and
an isolated script fixture; it is not a new live Rutube verification.

Section 3 completed on `codex/fix-metadata-validation`:

- `internal/http/client` owns a small injectable request interface and bounded
  reads. Requests have 45-second deadlines and default clients have 30-second TLS
  handshake limits. Metadata and playlist bodies are limited to 8 MiB, including unknown-length
  streams, and always close after success, status errors, truncation or read errors.
- Rutube's injected metadata client accepts context explicitly for video and playlist
  operations. HLS master readers expose the same cancellation seam, and media
  readers accept the injected HTTP reader. Existing CLI entry points delegate to
  these bounded readers; metadata/playlist retries retain their existing two-minute
  operation deadline and three-attempt policy.
- Video metadata requires a nonblank title and an absolute HTTP(S) playlist URL
  without credentials. Playlist titles, result arrays, video IDs and required
  continuation URLs are validated. Explicit empty lists remain valid, and valid
  titles/URLs are preserved.
- Playlist pagination resolves relative next links against final redirected URLs,
  detects cycles, stops at 10,000 pages and limits total response data to 32 MiB.
- Segment transfers use the injectable request contract and configured default
  transport while retaining their existing four-attempt retry and timeout policy.

Tests cover HTTP 404/503, HTTP 200 error JSON, malformed/missing fields, successful
metadata, empty/final/continuing pages, preserved read errors, truncated and
oversized bodies, closure without parsing error responses, redirects, cyclic and
oversized pagination, stalled headers/bodies, caller deadlines and cancellation.
Local HTTP fixtures and injected readers require no live Rutube or FFmpeg. The
existing segment retry and cleanup regressions remain in the ordinary test suite.

Build, lint and race checks validate section 3. Fifty race-enabled repetitions of
the cancellation checks passed, verifying that requests and fixture handlers finish
before cleanup.

Review follow-up: playlist pages now require an explicit boolean `has_next`;
missing or null state is rejected instead of silently treating a page as final.
Regression fixtures cover these cases alongside valid empty and final pages.
Default HTTP clients are explicitly owned by metadata, HLS playlist and segment
operations, which close idle connections on return; injected readers remain
caller-owned. Local connection-state fixtures verify reuse within an operation
and closure, and metadata entry-point tests verify cleanup. Cancellation tests
stop and join their workers on failure paths. The three test-only default-client
request wrappers and their obsolete transport-installation helper were removed;
status tests inject readers directly. Request errors identify video metadata,
playlist titles, numbered playlist pages, master playlists or media playlists
while preserving wrapped errors. Fifty race-enabled repetitions of connection
cleanup and cancellation checks passed after these changes.

Pipeline-wide shutdown, output safety and other later sections remain separate
work. Live Rutube checks
were not rerun for this HTTP/validation change.

## 4. Propagate pipeline errors and own temporary data

- [x] Retry interrupted segment transfers and temporary HTTP failures with bounded
  attempts, exponential backoff, and per-attempt and per-segment deadlines.
- [x] Remove partial segment files between attempts and remove the operation's
  temporary directory when a download fails.
- [ ] Propagate producer and processor errors to the caller instead of discarding
  them or requiring errors to be embedded in result values.
- [ ] Cancel outstanding work after a failure and wait for workers to finish
  before returning.
- [ ] Preserve segment ordering despite concurrent completion.
- [ ] Give one download operation ownership of its temporary directory and remove
  the directory and its contents on every exit path.
- [ ] Update progress after recording each completed segment so it reaches 100%.

Tests:

- Out-of-order completion, producer failures, segment failures, interrupted reads,
  cancellation, and cleanup after partial downloads.
- Verify conversion never starts after a download error.
- Verify all workers exit and cleanup happens only after they stop writing.
- Verify progress reaches 100%, including a one-segment download.
- Run concurrency tests with race detection.

## 5. Make output naming and FFmpeg conversion safe

- [ ] Sanitize titles into filenames and keep output inside the selected directory.
- [ ] Fail before downloading if the destination exists, unless overwrite is
  explicitly requested; apply the same policy to duplicate playlist titles.
- [ ] Use a unique concat manifest inside the operation's temporary directory.
- [ ] Escape segment paths correctly for FFmpeg concat syntax.
- [ ] Stop the spinner on every return path and report success only after the
  output is validated.
- [ ] Capture FFmpeg diagnostics in conversion errors.
- [ ] Write to a temporary MP4, validate the result, and publish it without
  overwriting an existing destination unless explicitly authorized.
- [ ] Detect unsuccessful conversion even when FFmpeg returns exit code zero
  without creating the expected output.

Tests:

- Titles containing slashes, traversal components, apostrophes, Unicode, newlines,
  and leading hyphens.
- Existing destinations, duplicate titles, and concurrent conversions.
- Missing FFmpeg, nonzero exit, and exit zero without a usable output file.
- Verify existing files survive failures, temporary artifacts disappear, and the
  spinner stops on both success and failure.
- Add a small integration test that uses real FFmpeg to generate local segments,
  concatenate them, and inspect the resulting MP4 with FFprobe.
- Keep ordinary unit tests independent of an installed FFmpeg executable.

## 6. Make developer checks safe and repeatable

- [ ] Restrict `make clean` to known generated artifacts instead of deleting every
  root-level MP4 and JSON file.
- [ ] Run download smoke tests in isolated output directories.
- [ ] Preserve mutating `make lint`; make CI detect unexpected changes after it runs.
- [ ] Keep live Rutube smoke tests opt-in.

Tests:

- Run cleanup in a temporary fixture directory and verify unrelated JSON and MP4
  files survive.
- Run the complete local integration flow without accessing Rutube.
- Verify CI fails when lint applies changes that were not committed.

## Final acceptance criteria

- [ ] `make lint` reports zero issues; a second run produces no changes.
- [ ] `make test` passes with race detection.
- [ ] Parser fuzz runs complete without crashes.
- [ ] A local HTTP → download → FFmpeg integration produces a valid MP4.
- [ ] Failure tests leave no partial output, temporary data, or active workers.
- [ ] Concurrent downloads preserve their own manifests, segments, and outputs.

Start with URL and HLS parsing: these bugs prevent valid downloads and the parser
fixtures provide the foundation for reliable end-to-end tests.

## Rules-based review after package reorganization

The repository now uses the conventions in `AGENTS.md`, adapted from Sugarwatch.
The reorganization changes package boundaries and removes unused infrastructure;
it does not complete the behavioral fixes above.

Completed structure changes:

- [x] Move the executable entry point to `cmd/dai` and update `make build`.
- [x] Replace `internal/run` with `internal/cli` and pass command arguments from
  the entry point instead of reading process-global arguments inside the CLI.
- [x] Replace `internal/usecase` with `internal/download`.
- [x] Rename `internal/m3u` to `internal/hls`; separate resolution models, variant
  models and parsing, master playlist parsing, and segment downloads into files.
- [x] Replace the generic `internal/util` package with `internal/progress` and
  move pipeline execution into `internal/pipeline`.
- [x] Keep each pipeline stage's interface, callback contract, constructor, and
  implementation in its own file.
- [x] Remove the unused worker-pool implementation and unused stage field.

Original findings from source inspection (completion is tracked in the stage
checklists above):

| Priority | Rule and finding | Location | Required verification |
| --- | --- | --- | --- |
| P1 | Goroutine ownership and bounded waits: the spinner outlives failed conversion; HTTP and FFmpeg operations lack cancellation and deadlines. Pipeline stop signals cannot interrupt callbacks or blocked HTTP reads. | `internal/progress/spinner.go`, `internal/ffmpeg/concat.go`, `internal/hls/download.go`, `internal/pipeline` | Stall HTTP reads and process execution, cancel with a deadline, and verify workers and progress reporting stop before the operation returns. |
| P1 | Output ownership: titles control output paths, concat manifests are shared across operations, and successful conversion leaves its temporary directory behind. Failed segment downloads now clean up their operation directory. | `internal/download/download.go`, `internal/ffmpeg/concat.go`, `internal/hls/download.go` | Isolate concurrent downloads; exercise invalid titles, existing destinations, partial downloads, and conversion failures; verify unrelated files survive. |
| P1 | Explicit failure contracts: pipeline callbacks discard errors; malformed HLS can panic; metadata HTTP failures can be accepted as success. | `internal/pipeline/produce_stage.go`, `internal/pipeline/process_stage.go`, `internal/hls`, `internal/rutube` | Public operation tests must observe the original failure and must not start conversion or report success. Add parser fuzz tests. |
| P2 | Small behavior interfaces: provider requests and FFmpeg execution are package-level calls bound to default clients and executables, making isolated orchestration tests difficult. | `internal/rutube`, `internal/ffmpeg`, `internal/download` | Introduce minimal owner-defined interfaces and constructors that validate and allocate without starting I/O; test orchestration through injected HTTP and conversion behavior. |
| P2 | Bounded input: response bodies are read with unbounded `io.ReadAll`, and pagination has no repeated-page detection or operation deadline. An oversized response can exhaust memory; repeated `next` values can loop indefinitely. | `internal/hls/playlist.go`, `internal/hls/media_playlist.go`, `internal/rutube/playlist_info.go`, `internal/rutube/video_info.go` | Add explicit metadata/playlist size limits and pagination safeguards; test oversized bodies and cyclic pagination with bounded completion. |
| P2 | Descriptive naming: legacy code still uses names such as `r`, `s`, `l`, `d`, `eBody`, and shadowing of the `bytes` package. Moving a file has not normalized its implementation. | `internal/hls`, `internal/progress/logger.go`, `internal/rutube`, `internal/ffmpeg/concat.go` | Rename identifiers when rewriting the affected implementation; run lint and tests without behavior changes. |
| P2 | Named constants: playlist line offsets, progress percentages, FFmpeg arguments, and display messages remain embedded in functions or have generic names. | `internal/hls`, `internal/ffmpeg/concat.go`, `internal/progress` | Replace positional parsing rather than naming incorrect offsets; give remaining domain values descriptive owner-local constants and verify behavior. |
| P2 | Safe repository checks: smoke tests still delete unrelated root files, and CI does not check whether mutating lint changed tracked files. | `Makefile`, `.github/workflows/lint.yml` | Test cleanup against unrelated fixture files and verify CI rejects uncommitted automatic fixes. |

Additional review constraints:

- [ ] Pass `context.Context` explicitly as the first argument of operations that
  need cancellation; never store it in structs or closure fields.
- [ ] Preserve minimal interfaces and return unexported implementations through
  interfaces from exported constructors. Keep construction separate from startup.
- [ ] Remove comments that repeat code when rewriting the surrounding code; use
  comments only to explain constraints, invariants, or upstream quirks.
- [ ] Keep signatures with four or more parameters on multiple lines.
- [ ] Add deterministic public-behavior tests for HLS, orchestration, conversion,
  and failure cleanup; existing parser unit tests alone do not validate downloads.

Validation of the reorganization must include build, lint, race tests, and repeated
pipeline tests that verify every item reaches the finalizer exactly once. Live
Rutube compatibility remains unverified unless the opt-in smoke tests are run.

## Fixed regression: phantom final segment

The HTTP status check introduced on this branch exposed the existing positional
media parser bug. For video `5d30a999322f6599f2a8f2883b1b3e6b`, the playlist has
1,416 segment URIs and a trailing newline. The old parser produced 1,417 entries;
the last was an empty URI, causing a request to the CDN directory. `master`
ignored the HTTP error body, whereas this branch failed the download.

The media parser now follows EXTINF entries, ignores blank lines and comments,
resolves segment references correctly, and rejects empty or truncated playlists.
The HTTP status check remains in place so real segment failures still abort.

Verification:

- The trailing-newline regression test reproduced HTTP 502 before the fix.
- Public download tests cover trailing newlines, no trailing newline, CRLF,
  variable headers, relative/root-relative/absolute segment URLs, malformed
  playlists, and genuine HTTP 502 segment failures.
- A temporary live check of the reported video selected the best variant and
  parsed exactly 1,416 segments. A 1 KiB sample from the final real segment
  returned HTTP 206. This does not validate a full video download or conversion.

## Fixed failure: interrupted segment transfers

A segment response can end prematurely with `unexpected EOF`. The downloader
previously aborted on the first interruption; `master` ignored the copy error and
could pass a truncated segment to FFmpeg.

Segment downloads now make at most four attempts. Unexpected EOF, short declared
content length, network errors, and HTTP 408/429/500/502/503/504 are retried with
250 ms, 500 ms, and 1 s delays. Each attempt has a 30 s deadline and the entire
segment operation has a 2 min deadline. HTTP 404, file creation/write/close errors,
and cancellation are not retried. Failure messages identify the segment and the
number of attempts and preserve the underlying error.

Each failed attempt closes its response and file and removes partial data before
another attempt creates the segment again. Exhausted downloads remove their
operation's temporary directory after all pipeline workers finish.

Deterministic tests cover recovery from unexpected EOF, temporary HTTP 502, and
short content length; replacement of longer partial data with the complete
response; bounded exhaustion; closed response bodies; permanent errors;
cancellation during backoff; and cleanup after public download failure. Retry
unit tests inject waits to verify the backoff without sleeping.

Live end-to-end verification of video `5d30a999322f6599f2a8f2883b1b3e6b` completed
successfully in an isolated temporary directory in 145.3 seconds. The rebuilt CLI
downloaded and converted the video with exit status zero. FFprobe reported H.264
video at 1280x720, AAC audio, duration 5696.882698 seconds, and a 961773660-byte MP4.
All live-test artifacts were removed afterward. Build, lint, race tests, and 50
repetitions of the deterministic segment retry tests also passed.

## Metadata retries and split live tests

The supplied retry helper is exposed as `retry.Do` and used for video
metadata, complete playlist metadata, master playlists, and media playlists.
These operations have a two-minute context deadline and at most three attempts.
Cancellation is checked before attempts; backoff waits one and two seconds.
Segment downloads retain their existing specialized retries without nesting the
generic helper around them.

Tests cover timeout recovery, interrupted playlist bodies, cancellation, exhausted
attempts, and restarting playlist metadata without duplicating earlier pages.

`make e2e-single` and `make e2e-playlist` run independently and stream CLI progress
and checksum results directly to the terminal. `make e2e` runs both. The real single-video checksum
test passed. The colon-prefixed playlist filename conversion bug is fixed below.

## Fixed failure: output title interpreted as a protocol

The first E2E playlist title begins with `6:57`. Passing that relative filename
directly to FFmpeg causes `Protocol not found` and exit status 8. The same valid
generated transport stream converts successfully when the output path is
absolute, independently of Rutube metadata or network downloads.

Conversion now resolves the output filename to an absolute path, preserves FFmpeg
diagnostics in returned errors, and stops the spinner before reporting either
success or failure. Error paths do not print READY.

Real FFmpeg regression tests cover plain, colon-prefixed, and leading-hyphen
filenames, plus invalid-input diagnostics and failure reporting:

```sh
go test -tags=integration -race -timeout=60s ./internal/ffmpeg
```

The live playlist test converted its first two videos successfully after the fix,
then stopped on a media-playlist TLS handshake timeout for the third video after
the configured retries. The complete playlist checksum run has not passed yet;
its reference hashes were not changed.
