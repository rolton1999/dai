# Download behavior

[Back to the README](../README.md)

Each MP4 is validated with FFprobe before atomic publication. Failed segments or
FFmpeg input/conversion errors fail the operation. Ctrl-C or the first failure
stops further scheduling and cancels active work; workers finish cleanup before
exit. Completed playlist videos remain available if another video fails.

The MP4 duration must cover the sum of HLS segment durations, allowing 5% of that
duration (capped at 250 ms) for timestamp and muxer rounding. Shorter output
fails validation and is removed before publication or recording a completed
resume entry. This check detects
duration loss; it does not verify every decoded frame or detect substituted
content of the same duration. Inaccurate upstream duration metadata can also
cause rejection.

Publication without overwrite uses a hard link or, on macOS and Linux, an atomic
exclusive rename when links are unsupported. Both refuse existing destinations,
including files created by another downloader during the operation. DAI probes
the output filesystem before downloading video data and fails early if neither
method is supported. Overwrite uses atomic rename directly.

Existing destinations require `--overwrite`. Duplicate playlist filenames are
rejected before downloading, even with overwrite enabled, using the destination
filesystem's case and Unicode normalization rules. Titles become filenames:
separators and control characters are replaced with underscores, surrounding
dots and spaces are trimmed, and leading hyphens receive an underscore prefix.
Names are limited to 200 UTF-8 bytes before `.mp4`; empty names become `video.mp4`.

Use `--resume` from the first playlist run. DAI keeps a versioned
`.dai-playlist-<ID>.json` manifest and a `.lock` file in the output directory.
Completed MP4s are verified by size and SHA-256. Missing or unfinished videos
restart from the beginning; fragments are never reused. Recorded filenames stay
stable if titles change. Changed, untracked, or symlink outputs cause an error
and are preserved; move conflicting files aside to continue. Resume is
playlist-only and cannot combine with `--overwrite`. The lock prevents concurrent
resume runs for the same playlist and releases on process exit. Keep the manifest
with the downloaded files; completed runs retain it for future reuse.

Interactive terminals show independent progress rows and conversion spinners.
Small terminals prioritize active videos and recent completions. Redirected
output uses completion lines in playlist order.

DAI supports complete, unencrypted MPEG-TS HLS playlists in segment order.
Resolution determines variant selection; bandwidth breaks ties. Relative URLs
resolve after redirects. Separate media tracks, encryption, byte ranges,
initialization maps, and live or partial playlists are rejected.
