# Bug fix plan

This plan covers the remaining bugs identified in the project review. Complete
the stages in order, keeping `make lint` and `make test` passing after each stage.

## 1. Parse and validate input URLs

- [ ] Use `net/url` to validate the host and exact path structure.
- [ ] Extract video and playlist IDs independently of queries and fragments.
- [ ] Reject missing IDs and misleading paths such as `/videosomething/`.

Tests:

- Table-driven tests for video and playlist links with trailing slashes, queries,
  fragments, missing IDs, incorrect hosts, and invalid paths.
- Keep the existing no-argument regression test.

## 2. Replace positional HLS parsing

- [ ] Parse tags and URI lines explicitly instead of assuming fixed line offsets.
- [ ] Read variant attributes by name, respecting quoted commas.
- [ ] Support LF, CRLF, blank lines, comments, optional tags, and reordered attributes.
- [ ] Resolve variant and segment references against the final response URL after
  redirects, including relative, root-relative, and absolute references.
- [ ] Return errors for empty, malformed, or unsupported playlists instead of
  panicking or producing incomplete output.
- [ ] Make best-variant selection reject empty lists and invalid resolutions.
- [ ] Explicitly reject unsupported encryption, byte ranges, and separate audio
  tracks until those features are implemented.

Tests:

- Table-driven master and media fixtures covering trailing newlines, variable
  headers, comments, CRLF, reordered or omitted optional attributes, and quoted
  codec lists.
- URL resolution tests, including redirects.
- Empty playlists, truncated entries, malformed attributes, invalid resolutions,
  and unsupported playlist features must produce clear errors.
- Fuzz both parsers: arbitrary input must never panic.

## 3. Bound HTTP requests and report failures explicitly

- [ ] Introduce an injectable HTTP client with timeouts and context cancellation.
- [ ] Check HTTP status before parsing metadata or playlists.
- [ ] Validate required metadata fields instead of accepting error JSON as an
  empty successful response.
- [ ] Close response bodies on every success and failure path, including master
  playlist responses.

Tests:

- Local HTTP servers or fake transports for 404/503 responses, malformed JSON,
  missing fields, truncated bodies, redirects, and stalled requests.
- Track response body closure on success and failure.
- Verify a missing playlist returns an error instead of an empty success.
- Verify timeouts and cancellation finish within bounded deadlines.

## 4. Propagate pipeline errors and own temporary data

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

Remaining findings from source inspection:

| Priority | Rule and finding | Location | Required verification |
| --- | --- | --- | --- |
| P1 | Goroutine ownership and bounded waits: the spinner outlives failed conversion; HTTP and FFmpeg operations lack cancellation and deadlines. Pipeline stop signals cannot interrupt callbacks or blocked HTTP reads. | `internal/progress/spinner.go`, `internal/ffmpeg/concat.go`, `internal/hls/download.go`, `internal/pipeline` | Stall HTTP reads and process execution, cancel with a deadline, and verify workers and progress reporting stop before the operation returns. |
| P1 | Output ownership: titles control output paths, concat manifests are shared across operations, and failed downloads leave temporary data behind. | `internal/download/download.go`, `internal/ffmpeg/concat.go`, `internal/hls/download.go` | Isolate concurrent downloads; exercise invalid titles, existing destinations, partial downloads, and conversion failures; verify unrelated files survive. |
| P1 | Explicit failure contracts: pipeline callbacks discard errors; malformed HLS can panic; metadata HTTP failures can be accepted as success. | `internal/pipeline/produce_stage.go`, `internal/pipeline/process_stage.go`, `internal/hls`, `internal/rutube` | Public operation tests must observe the original failure and must not start conversion or report success. Add parser fuzz tests. |
| P2 | Small behavior interfaces: provider requests and FFmpeg execution are package-level calls bound to default clients and executables, making isolated orchestration tests difficult. | `internal/rutube`, `internal/ffmpeg`, `internal/download` | Introduce minimal owner-defined interfaces and constructors that validate and allocate without starting I/O; test orchestration through injected HTTP and conversion behavior. |
| P2 | Bounded input: response bodies are read with unbounded `io.ReadAll`, and pagination has no repeated-page detection or operation deadline. An oversized response can exhaust memory; repeated `next` values can loop indefinitely. | `internal/hls/playlist.go`, `internal/hls/download.go`, `internal/rutube/playlist_info.go`, `internal/rutube/video_info.go` | Add explicit metadata/playlist size limits and pagination safeguards; test oversized bodies and cyclic pagination with bounded completion. |
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
