# DAI coding conventions

Adapted from `../sugar/AGENTS.md`. Apply these rules when changing this repository.
Use the tools and checks present here; instructions copied from other projects do
not define this repository's build or lint commands.

## Project structure and responsibilities

DAI is a Go command-line downloader for Rutube videos and playlists. It fetches
metadata, selects an HLS variant, downloads segments, and invokes FFmpeg to create
an MP4. It has no database, HTTP server, or browser frontend.

| Package or folder | Responsibility |
| --- | --- |
| `cmd/dai` | Executable entry point, process exit status, and top-level error reporting. |
| `internal/cli` | Command-line argument validation and dispatch. |
| `internal/download` | Video and playlist download orchestration and operation-level resource ownership. |
| `internal/rutube` | Rutube metadata API requests, response parsing, and shared metadata models. |
| `internal/hls` | HLS playlist parsing, variant selection, segment metadata, and segment downloads. |
| `internal/ffmpeg` | FFmpeg invocation and concat manifest handling. |
| `internal/progress` | Terminal progress reporting and spinner lifecycle. |
| `internal/retry` | Context-aware retry execution and backoff. |
| `internal/pipeline` | Bounded concurrent stage execution and stage contracts. |

Keep process wiring in `cmd/dai`, argument handling in `internal/cli`, provider
behavior in `internal/rutube`, HLS behavior in `internal/hls`, and process invocation
in `internal/ffmpeg`. Avoid generic `util`, `run`, and `usecase` packages. Keep
interfaces and models with the behavior that owns them; do not create a shared
catch-all contracts package.

At the repository root, `Makefile` owns build, pinned lint, and test commands;
`.golangci.yml` defines lint and formatting rules; `.github/workflows/lint.yml`
runs checks in CI; and `bugs.md` records known defects and their test plan.
The package table describes responsibilities; unresolved ownership defects remain
tracked in `bugs.md` until implementation and tests are complete.

## Go API and file organization

- Use descriptive names for variables, parameters and receivers. Do not shorten
  names to initials or single letters such as `r`, `w`, `p`, or `c`; use
  `downloadWorker`, `request`, `responseWriter`, or `connection` instead. Apply
  this rule to new and rewritten code, including tests.

- Use named constants for fixed values: defaults, limits, durations, permissions,
  routes, header names, environment keys, log categories and messages. Do not
  embed these literals in functions, even when used only once. Prefer existing
  standard-library constants when they express the value. Keep constants in the
  package that owns their meaning; use descriptive names rather than a generic
  constants package. Apply this rule to new and rewritten code, including tests.
  Empty values and ordinary zero/one bookkeeping may stay inline; values with
  domain meaning must have names.

- Declare every struct field on its own line. Do not group field names in a
  declaration such as `width, height int`, or put non-empty structs on one line.
  Apply this rule to named and anonymous structs in production code and tests;
  empty structs may remain `struct{}`.

- Do not nest constructor or factory calls inside other constructor or factory
  calls. Create each dependency in a descriptive local variable first, then pass
  that variable to the outer constructor. Apply this rule to production code,
  tests and standard-library wrappers such as readers and closers.

- Export the behavior callers need through small interfaces. Keep concrete
  implementations unexported and return an interface from exported constructors.
  Configuration structs, shared protocol/data structs, sentinel errors and
  persisted constants may be exported when callers need them.
- Declare a shared contract once in the package that owns the behavior. A sole
  consumer may declare a narrow view; move it to the owner when it becomes shared.
  Keep interfaces minimal: expose only methods callers use, and fold methods that
  differ only by a fixed argument into one parameterized method. Do not add
  compile-time interface assertion blocks.
- Keep each implementation's methods in one file. With one implementation, keep
  its interface and methods together. With several, keep the interface in the
  main file and put each implementation in its own file.
- Order declarations as interface, constructor, public methods, then helpers.
  Put every parameter on its own line for signatures with four or more
  parameters, including interface methods.

Keep logically related statements together and separate distinct blocks with blank
lines. `wsl_v5` enforces this spacing across all Go packages through `.golangci.yml`.
Use its default checks plus `after-block`; related assignment/error-check pairs
may remain together.

## Lifecycle and comments

- **Hard prohibition: never store `context.Context` in any struct field.** This
  applies to production code, tests, mocks, embedded fields and type aliases.
  Pass context explicitly as the first argument to every method or worker that
  needs it. Do not hide a stored context behind another type or a closure field.
  A lifecycle owner may store a `context.CancelFunc` or completion channel;
  it must still stop and join its work. `containedctx` is a required lint check:
  do not disable it or suppress its findings to bypass this rule.
- Constructors validate and allocate. Start network I/O, background work,
  goroutines and timers explicitly after construction.
- Every goroutine has an owner that stops and joins it on every exit path.
  Bound waits with deadlines. Long-lived work needs a shutdown path that stops
  intake and waits for in-flight work. Use a bounded worker pool for long-lived
  job processing rather than starting a goroutine per job. Join short-lived
  fan-out work on every exit path, including errors.
- Prefer clear names over comments. New and edited comments must be in English
  and explain a constraint, invariant, upstream quirk or workaround. Remove
  comments that repeat code or signatures. Do not comment mocks, stubs or test
  doubles. Leave existing non-English comments alone unless rewriting the
  surrounding code.

## Checks and tests

- Run `make build`, `make lint`, and `make test` before pushing.
- `make lint` applies automatic fixes. Inspect those changes and rerun checks when
  they affect behavior. Keep `containedctx` enabled.
- `make test` runs Go tests with race detection. Prefer observable behavior over
  implementation details. Keep tests independent of live Rutube and an installed
  FFmpeg unless they are explicitly designated integration or smoke tests.
- Use local HTTP fixtures and injectable process execution for failure cases.
  Real FFmpeg integration tests must generate their own local inputs and isolate
  all output in temporary directories.
- Treat intermittent failures as defects. Isolate mutable test data with unique
  identifiers. Do not use `time.Sleep` in tests; wait for observable conditions
  with deadlines. Prefer fake clocks for expiry tests. Run concurrency changes
  with the race detector and a high repeat count to expose timing defects; never
  use retries, skips, or repeat counts to excuse intermittent failures.
- `make test-small` and `make test-big` access live Rutube and require FFmpeg.
  They are opt-in smoke tests, not substitutes for deterministic automated tests.
  Their current cleanup behavior is a known defect tracked in `bugs.md`.

```sh
make build
make lint
make test
```

## Product and output data

- Keep repository documentation in English.
- Preserve Rutube video and playlist download behavior when reorganizing code.
  Changes to accepted URLs, supported HLS features, and output naming need
  explicit tests and documentation.
- Never treat an HTTP error, unsupported playlist, failed segment, or incomplete
  conversion as successful output.
- Preserve existing user files. Cleanup must affect only artifacts owned by the
  operation; live smoke tests must not delete unrelated files.
- Document tested scenarios and unresolved gaps in `bugs.md`. Local HTTP fixtures
  and process doubles do not prove compatibility with the live Rutube service.
