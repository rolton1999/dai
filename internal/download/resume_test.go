package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dai/internal/progress"
	"dai/internal/rutube"
)

const (
	resumeTestUnexpected    = "unexpected recovery result: %v"
	resumeTestStarts        = "downloaded %d videos, expected %d"
	resumeTestMetadata      = "metadata calls=%v, expected %d"
	resumeTestChangedOutput = "completed output changed: %q, %v"
	resumeTestChangedTime   = "completed output was replaced"
	resumeTestNoState       = "missing checkpoint for %s: %+v"
	resumeTestChangedTitle  = "-renamed"
	resumeTestModifiedBody  = "modified output"
	resumeTestMissingOutput = "missing output"
	resumeTestUnknownOutput = "untracked output"
	resumeTestPlannedOutput = "uncommitted output"
	resumeTestSymlinkOutput = "symlink output"
	resumeTestBrokenState   = "broken state"
	resumeTestOtherPlaylist = "other-playlist"
	resumeTestTraversalName = "../outside.mp4"
	resumeTestInvalidJSON   = "{broken"
	resumeTestBadVersion    = 999
	resumeTestStagingName   = "converted.mp4"
	resumeTestCount         = 2
	resumeTestManifestKept  = "manifest was modified after validation failed"
	resumeTestBusyLock      = "concurrent recovery acquired the same lock"
	resumeTestUnrelated     = "unrelated.mp4"
)

type recordingResumeProvider struct {
	provider *parallelProvider
	calls    []string
	suffix   string
}

func (provider *recordingResumeProvider) Video(operationContext context.Context, identifier string) (rutube.VideoInfo, error) {
	provider.calls = append(provider.calls, identifier)
	info, err := provider.provider.Video(operationContext, identifier)
	info.Title += provider.suffix

	return info, err
}

func (provider *recordingResumeProvider) Playlist(operationContext context.Context, identifier string) (rutube.PlaylistInfo, error) {
	return provider.provider.Playlist(operationContext, identifier)
}

func runCompletedResumeFixture(test *testing.T, directory string, count int, suffix string) (*parallelStreams, *recordingResumeProvider) {
	test.Helper()
	provider, streams, converter := newParallelFixture(test, count)
	recorder := &recordingResumeProvider{provider: provider, suffix: suffix}

	for _, release := range converter.releases {
		release <- nil
	}

	logger := &fixtureLogger{}

	service, err := New(recorder, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	operationContext, cancel := context.WithTimeout(test.Context(), parallelDeadline)
	defer cancel()

	if err := service.Playlist(operationContext, parallelPlaylistID, Options{OutputDirectory: directory, Resume: true}); err != nil {
		test.Fatalf(resumeTestUnexpected, err)
	}

	return streams, recorder
}

func readResumeManifest(test *testing.T, directory string) resumeManifest {
	test.Helper()

	path := filepath.Join(directory, fmt.Sprintf(resumeFileFormat, parallelPlaylistID))

	body, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}

	var manifest resumeManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		test.Fatal(err)
	}

	return manifest
}

func TestResumeManifestReloadsGeneratedFilenames(test *testing.T) {
	const (
		titleCharacter      = "a"
		titleSuffix         = "suffix"
		dotBoundary         = "."
		spaceBoundary       = " "
		unicodeBoundary     = "界"
		unicodePrefixLength = 197
		filenameMismatch    = "recorded filename changed: got %q, want %q"
	)

	for _, boundary := range []string{dotBoundary, spaceBoundary, unicodeBoundary} {
		test.Run(boundary, func(test *testing.T) {
			directory := test.TempDir()

			title := strings.Repeat(titleCharacter, outputNameLimit-1) + boundary + titleSuffix
			if boundary == unicodeBoundary {
				title = strings.Repeat(titleCharacter, unicodePrefixLength) + boundary + titleSuffix
			}

			filename := safeFilename(title)

			recovery, err := newPlaylistRecovery(directory, parallelPlaylistID)
			if err != nil {
				test.Fatal(err)
			}

			addError := recovery.add(testID, filename)
			saveError := recovery.save()

			closeError := recovery.Close()
			if err := errors.Join(addError, saveError, closeError); err != nil {
				test.Fatal(err)
			}

			reloaded, err := newPlaylistRecovery(directory, parallelPlaylistID)
			if err != nil {
				test.Fatal(err)
			}

			defer func() {
				if err := reloaded.Close(); err != nil {
					test.Error(err)
				}
			}()

			if actual := reloaded.filename(testID); actual != filename {
				test.Fatalf(filenameMismatch, actual, filename)
			}
		})
	}
}

func TestPlaylistResumeRestartsOnlyUnfinishedVideos(test *testing.T) {
	operationContext, cancel := context.WithTimeout(test.Context(), parallelDeadline)
	defer cancel()

	directory := test.TempDir()
	provider, streams, converter := newParallelFixture(test, parallelItemCount)
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	results := make(chan error, 1)

	var workers sync.WaitGroup

	test.Cleanup(func() {
		cancel()
		workers.Wait()
	})
	workers.Go(func() {
		results <- service.Playlist(operationContext, parallelPlaylistID, Options{OutputDirectory: directory, Resume: true})
	})

	completed := waitParallelStart(test, operationContext, converter.started)
	for range parallelWorkerCount - 1 {
		waitParallelStart(test, operationContext, converter.started)
	}

	converter.releases[completed] <- nil

	waitParallelStart(test, operationContext, converter.started)
	cancel()

	joinContext, stop := context.WithTimeout(test.Context(), parallelDeadline)
	defer stop()

	if err := waitParallelResult(test, joinContext, results); !errors.Is(err, context.Canceled) {
		test.Fatalf(resumeTestUnexpected, err)
	}

	manifest := readResumeManifest(test, directory)
	if manifest.Videos[completed].SHA256 == "" {
		test.Fatalf(resumeTestNoState, completed, manifest)
	}

	target := filepath.Join(directory, completed+outputExtension)

	before, err := os.Stat(target)
	if err != nil {
		test.Fatal(err)
	}

	restarted, recorder := runCompletedResumeFixture(test, directory, parallelItemCount, "")
	if restarted.started.Load() != parallelItemCount-1 {
		test.Fatalf(resumeTestStarts, restarted.started.Load(), parallelItemCount-1)
	}

	if len(recorder.calls) != parallelItemCount-1 {
		test.Fatalf(resumeTestMetadata, recorder.calls, parallelItemCount-1)
	}

	after, err := os.Stat(target)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		test.Fatal(resumeTestChangedTime)
	}

	body, err := os.ReadFile(target)
	if err != nil || string(body) != completed {
		test.Fatalf(resumeTestChangedOutput, body, err)
	}

	again, recorder := runCompletedResumeFixture(test, directory, parallelItemCount, resumeTestChangedTitle)
	if again.started.Load() != 0 || len(recorder.calls) != 0 {
		test.Fatalf(resumeTestStarts, again.started.Load(), 0)
	}
}

func TestPlaylistResumeRedownloadsMissingOutputUsingRecordedName(test *testing.T) {
	directory := test.TempDir()
	runCompletedResumeFixture(test, directory, resumeTestCount, "")

	identifier := fmt.Sprintf(parallelIDFormat, 0)

	target := filepath.Join(directory, identifier+outputExtension)
	if err := os.Remove(target); err != nil {
		test.Fatal(err)
	}

	streams, recorder := runCompletedResumeFixture(test, directory, resumeTestCount, resumeTestChangedTitle)
	if streams.started.Load() != 1 || len(recorder.calls) != 1 || recorder.calls[0] != identifier {
		test.Fatalf(resumeTestStarts, streams.started.Load(), 1)
	}

	body, err := os.ReadFile(target)
	if err != nil || string(body) != identifier {
		test.Fatalf(resumeTestChangedOutput, body, err)
	}
}

func TestPlaylistResumeRejectsUnverifiedFilesWithoutReplacingThem(test *testing.T) {
	for _, scenario := range []string{resumeTestModifiedBody, resumeTestUnknownOutput, resumeTestPlannedOutput, resumeTestSymlinkOutput} {
		test.Run(scenario, func(test *testing.T) {
			directory := test.TempDir()
			runCompletedResumeFixture(test, directory, 1, "")

			identifier := fmt.Sprintf(parallelIDFormat, 0)
			target := filepath.Join(directory, identifier+outputExtension)
			manifestPath := filepath.Join(directory, fmt.Sprintf(resumeFileFormat, parallelPlaylistID))
			expected := identifier

			switch scenario {
			case resumeTestModifiedBody:
				expected = strings.Repeat("x", len(identifier))
				if err := os.WriteFile(target, []byte(expected), testPermissions); err != nil {
					test.Fatal(err)
				}
			case resumeTestUnknownOutput:
				if err := os.Remove(manifestPath); err != nil {
					test.Fatal(err)
				}
			case resumeTestPlannedOutput:
				manifest := readResumeManifest(test, directory)
				manifest.Videos[identifier] = resumeVideo{Filename: identifier + outputExtension}
				writeResumeTestManifest(test, manifestPath, manifest)
			case resumeTestSymlinkOutput:
				unrelated := filepath.Join(directory, resumeTestUnrelated)
				if err := os.Rename(target, unrelated); err != nil {
					test.Fatal(err)
				}

				if err := os.Symlink(unrelated, target); err != nil {
					test.Fatal(err)
				}
			}

			provider, streams, converter := newParallelFixture(test, 1)
			logger := &fixtureLogger{}

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			if err := service.Playlist(test.Context(), parallelPlaylistID, Options{OutputDirectory: directory, Resume: true}); err == nil || streams.started.Load() != 0 {
				test.Fatalf(resumeTestUnexpected, err)
			}

			body, err := os.ReadFile(target)
			if err != nil || string(body) != expected {
				test.Fatalf(resumeTestChangedOutput, body, err)
			}
		})
	}
}

func writeResumeTestManifest(test *testing.T, path string, manifest resumeManifest) {
	test.Helper()

	body, err := json.Marshal(manifest)
	if err != nil {
		test.Fatal(err)
	}

	if err := os.WriteFile(path, body, testPermissions); err != nil {
		test.Fatal(err)
	}
}

func TestPlaylistResumeRejectsInvalidStateBeforeDownloads(test *testing.T) {
	const (
		invalidVersion  = "unsupported version"
		wrongPlaylist   = "wrong playlist"
		unsafeFilename  = "unsafe filename"
		invalidChecksum = "invalid checksum"
	)
	for _, scenario := range []string{resumeTestBrokenState, invalidVersion, wrongPlaylist, unsafeFilename, invalidChecksum} {
		test.Run(scenario, func(test *testing.T) {
			directory := test.TempDir()
			runCompletedResumeFixture(test, directory, 1, "")
			manifestPath := filepath.Join(directory, fmt.Sprintf(resumeFileFormat, parallelPlaylistID))
			manifest := readResumeManifest(test, directory)

			switch scenario {
			case resumeTestBrokenState:
				if err := os.WriteFile(manifestPath, []byte(resumeTestInvalidJSON), testPermissions); err != nil {
					test.Fatal(err)
				}
			case invalidVersion:
				manifest.Version = resumeTestBadVersion
				writeResumeTestManifest(test, manifestPath, manifest)
			case wrongPlaylist:
				manifest.PlaylistID = resumeTestOtherPlaylist
				writeResumeTestManifest(test, manifestPath, manifest)
			case unsafeFilename, invalidChecksum:
				for identifier, video := range manifest.Videos {
					if scenario == unsafeFilename {
						video.Filename = resumeTestTraversalName
					} else {
						video.SHA256 = resumeTestInvalidJSON
					}

					manifest.Videos[identifier] = video
				}

				writeResumeTestManifest(test, manifestPath, manifest)
			}

			before, err := os.ReadFile(manifestPath)
			if err != nil {
				test.Fatal(err)
			}

			provider, streams, converter := newParallelFixture(test, 1)
			logger := &fixtureLogger{}

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			if err := service.Playlist(test.Context(), parallelPlaylistID, Options{OutputDirectory: directory, Resume: true}); err == nil || streams.started.Load() != 0 {
				test.Fatalf(resumeTestUnexpected, err)
			}

			after, err := os.ReadFile(manifestPath)
			if err != nil || string(before) != string(after) {
				test.Fatal(resumeTestManifestKept)
			}
		})
	}
}

func TestPlaylistResumeRecognizesPublicationAfterCheckpoint(test *testing.T) {
	for _, published := range []bool{false, true} {
		test.Run(fmt.Sprint(published), func(test *testing.T) {
			directory := test.TempDir()
			identifier := fmt.Sprintf(parallelIDFormat, 0)
			filename := identifier + outputExtension

			recovery, err := newPlaylistRecovery(directory, parallelPlaylistID)
			if err != nil {
				test.Fatal(err)
			}

			if err := recovery.add(identifier, filename); err != nil {
				test.Fatal(err)
			}

			staging := filepath.Join(test.TempDir(), resumeTestStagingName)
			if err := os.WriteFile(staging, []byte(identifier), testPermissions); err != nil {
				test.Fatal(err)
			}

			target := filepath.Join(directory, filename)

			if err := recovery.prepare(test.Context(), identifier, staging); err != nil {
				test.Fatal(err)
			}

			if published {
				if err := os.WriteFile(target, []byte(identifier), testPermissions); err != nil {
					test.Fatal(err)
				}
			}

			if err := recovery.Close(); err != nil {
				test.Fatal(err)
			}

			streams, _ := runCompletedResumeFixture(test, directory, 1, "")

			expected := int32(1)
			if published {
				expected = 0
			}

			if streams.started.Load() != expected {
				test.Fatalf(resumeTestStarts, streams.started.Load(), expected)
			}
		})
	}
}

func TestPlaylistResumeLockIsReleasedOnClose(test *testing.T) {
	directory := test.TempDir()

	first, err := newPlaylistRecovery(directory, parallelPlaylistID)
	if err != nil {
		test.Fatal(err)
	}

	second, secondError := newPlaylistRecovery(directory, parallelPlaylistID)
	if secondError == nil {
		_ = second.Close()

		test.Error(resumeTestBusyLock)
	}

	if err := first.Close(); err != nil {
		test.Fatal(err)
	}

	third, err := newPlaylistRecovery(directory, parallelPlaylistID)
	if err != nil {
		test.Fatal(err)
	}

	if err := third.Close(); err != nil {
		test.Fatal(err)
	}
}

func TestResumeCheckpointFailureDoesNotPublishOutput(test *testing.T) {
	directory := test.TempDir()
	provider, streams, _ := newParallelFixture(test, 1)
	manifestPath := filepath.Join(directory, fmt.Sprintf(resumeFileFormat, parallelPlaylistID))
	converter := conversionAdapter(func(
		_ context.Context,
		_ progress.Logger,
		_ []string,
		output string,
	) error {
		if err := os.Remove(manifestPath); err != nil {
			return err
		}

		if err := os.Mkdir(manifestPath, outputDirectoryPermissions); err != nil {
			return err
		}

		return os.WriteFile(output, []byte(testContents), testPermissions)
	})
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	if err := service.Playlist(test.Context(), parallelPlaylistID, Options{OutputDirectory: directory, Resume: true}); err == nil {
		test.Fatalf(resumeTestUnexpected, err)
	}

	outputs, err := filepath.Glob(filepath.Join(directory, "*"+outputExtension))
	if err != nil || len(outputs) != 0 {
		test.Fatalf(parallelUnexpectedFiles, outputs, err)
	}

	temporary, err := filepath.Glob(filepath.Join(directory, resumeTemporaryPattern))
	if err != nil || len(temporary) != 0 || streams.closed.Load() != 1 {
		test.Fatalf(parallelUnexpectedFiles, temporary, err)
	}
}
