package download

import (
	"context"
	"dai/internal/hls"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/rutube"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testTitle       = "fixture"
	testID          = "video-id"
	testContents    = "complete MP4"
	testPermissions = 0o600
)

const (
	outputTestSuccess                              = "success"
	outputTestFailure                              = "failure"
	outputTestConversionFailed                     = "conversion failed"
	outputTestUnexpectedResult                     = "unexpected result: %v"
	outputTestArtifactNotClosed                    = "artifact not closed"
	outputTestPartialArtifacts                     = "partial artifacts: %v"
	outputTestUnexpectedFiles                      = "unexpected files: %v"
	outputTestExisting                             = "existing"
	outputTestDuplicate                            = "duplicate"
	outputTestErrorStreamCallsConversionCalls      = "error=%v stream calls=%d conversion calls=%d"
	outputTestExistingOutputChanged                = "existing output changed: %q %v"
	outputTestEscape                               = "../../escape"
	outputTestABC                                  = "a/b\\c"
	outputTestNameNext                             = "name\nnext"
	outputTestLeading                              = "-leading"
	outputTestQuoted                               = "'quoted'"
	outputTestUnicodeTitle                         = "Русский"
	outputTestWideCharacter                        = "界"
	outputTestEmptyTitle                           = ".."
	outputTestUnsafeCharacters                     = "/\\\r\n"
	outputTestHyphen                               = "-"
	outputTestUnsafeFilenameFor                    = "unsafe filename %q for %q"
	outputTestFixtureFailure                       = "fixture failure"
	outputTestFailedConversionAccepted             = "failed conversion accepted"
	outputTestOriginalOutputChanged                = "original output changed: %q %v"
	outputTestIncompleteCleanup                    = "incomplete cleanup: %v %v"
	outputTestConversionsFailedToReachPublication  = "conversions failed to reach publication"
	outputTestDownloadsDidNotJoin                  = "downloads did not join"
	outputTestSuccessfulCompetingPublications      = "successful competing publications: %d"
	outputTestArtifactRemainedOpen                 = "artifact remained open"
	outputTestPublicationCleanup                   = "publication cleanup: %v %v"
	outputTestWinnerOutput                         = "winner output: %q %v"
	outputTestFixtureSegmentFailure                = "fixture segment failure"
	outputTestError                                = "error=%v"
	outputTestConversionStartedAfterFailedDownload = "conversion started after failed download"
	outputTestPartialOutput                        = "partial output: %v %v"
)

const fixtureMasterURL = "https://example.test/master.m3u8"

const (
	outputFixtureOriginal         = "original output"
	outputFixtureInvalidSuccess   = "invalid conversion success accepted: %v"
	outputFixtureUnexpectedReady  = "failed operation reported READY: %v"
	outputFixtureFilenameMismatch = "title %q: expected %q, got %q"
	outputFixtureCleanupFailure   = "artifact cleanup failed"
	outputFixtureOverwriteFailure = "overwrite failed: output=%q, error=%v"
	outputFixtureCleanupErrorLost = "cleanup error lost: %v"
	outputFixtureEscapeTitle      = "../../escape"
	outputFixtureEscapeName       = "_.._escape.mp4"
	outputFixtureSeparatorTitle   = "a/b\\c"
	outputFixtureSeparatorName    = "a_b_c.mp4"
	outputFixtureNewlineTitle     = "name\nnext"
	outputFixtureNewlineName      = "name_next.mp4"
	outputFixtureHyphenTitle      = "-leading"
	outputFixtureHyphenName       = "_-leading.mp4"
	outputFixtureUnicodeTitle     = "Русский"
	outputFixtureUnicodeName      = "Русский.mp4"
	outputFixtureQuoteTitle       = "'quoted'"
	outputFixtureQuoteName        = "'quoted'.mp4"
	outputFixtureFallbackTitle    = ".."
	outputFixtureFallbackName     = "video.mp4"
)

const (
	outputTestNoOutput    = "no output"
	outputTestEmptyOutput = "empty output"
)

type fixtureProvider struct {
	title string
}

func (provider *fixtureProvider) Video(context.Context, string) (rutube.VideoInfo, error) {
	return rutube.VideoInfo{Title: provider.title}, nil
}

func (provider *fixtureProvider) Playlist(context.Context, string) (rutube.PlaylistInfo, error) {
	return rutube.PlaylistInfo{VideoIDs: []string{testID, testID}}, nil
}

type fixtureStreams struct {
	calls    int
	artifact *fixtureArtifact
	failure  error
}

func (streams *fixtureStreams) Variants(operationContext context.Context, _ string) ([]hls.VariantPlaylist, error) {
	streams.calls++
	reader := &variantFixtureReader{}
	streamsClient := hls.New(reader, nil)

	return streamsClient.Variants(operationContext, fixtureMasterURL)
}

func (streams *fixtureStreams) Download(context.Context, progress.Logger, hls.VariantPlaylist) (hls.Artifact, error) {
	return streams.artifact, streams.failure
}

type fixtureArtifact struct {
	closed  bool
	failure error
}

func (artifact *fixtureArtifact) Files() []string {
	return nil
}

func (artifact *fixtureArtifact) Close() error {
	artifact.closed = true
	return artifact.failure
}

type fixtureConverter struct {
	failure error
	calls   int
}

func (converter *fixtureConverter) Convert(
	_ context.Context,
	_ progress.Logger,
	_ []string,
	result string,
) error {
	converter.calls++

	if err := os.WriteFile(result, []byte(testContents), testPermissions); err != nil {
		return err
	}

	return converter.failure
}

func TestPublicationAndCleanup(test *testing.T) {
	for _, failure := range []bool{false, true} {
		test.Run(map[bool]string{false: outputTestSuccess, true: outputTestFailure}[failure], func(test *testing.T) {
			directory := test.TempDir()
			artifact := &fixtureArtifact{}
			streams := &fixtureStreams{artifact: artifact}

			converter := &fixtureConverter{}
			if failure {
				converter.failure = errors.New(outputTestConversionFailed)
			}

			provider := &fixtureProvider{title: testTitle}
			logger := &fixtureLogger{}

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			err = service.Video(context.Background(), testID, Options{OutputDirectory: directory})
			if (err != nil) != failure {
				test.Fatalf(outputTestUnexpectedResult, err)
			}

			if !artifact.closed {
				test.Fatal(outputTestArtifactNotClosed)
			}

			entries, err := os.ReadDir(directory)
			if err != nil {
				test.Fatal(err)
			}

			if failure && len(entries) != 0 {
				test.Fatalf(outputTestPartialArtifacts, entries)
			}

			if !failure {
				if len(entries) != 1 || entries[0].Name() != testTitle+outputExtension {
					test.Fatalf(outputTestUnexpectedFiles, entries)
				}
			}
		})
	}
}

func TestCollisionsFailBeforeDownload(test *testing.T) {
	for _, playlist := range []bool{false, true} {
		test.Run(map[bool]string{false: outputTestExisting, true: outputTestDuplicate}[playlist], func(test *testing.T) {
			directory := test.TempDir()

			target := filepath.Join(directory, testTitle+outputExtension)
			if !playlist {
				if err := os.WriteFile(target, []byte(testContents), testPermissions); err != nil {
					test.Fatal(err)
				}
			}

			streams := &fixtureStreams{}
			converter := &fixtureConverter{}

			provider := &fixtureProvider{title: testTitle}
			logger := &fixtureLogger{}

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			if playlist {
				err = service.Playlist(context.Background(), testID, Options{OutputDirectory: directory})
			} else {
				err = service.Video(context.Background(), testID, Options{OutputDirectory: directory})
			}

			if err == nil || streams.calls != 0 || converter.calls != 0 {
				test.Fatalf(outputTestErrorStreamCallsConversionCalls, err, streams.calls, converter.calls)
			}

			if !playlist {
				body, err := os.ReadFile(target)
				if err != nil || string(body) != testContents {
					test.Fatalf(outputTestExistingOutputChanged, body, err)
				}
			}
		})
	}
}

func TestSafeFilenames(test *testing.T) {
	for _, title := range []string{outputTestEscape, outputTestABC, outputTestNameNext, outputTestLeading, outputTestQuoted, outputTestUnicodeTitle, strings.Repeat(outputTestWideCharacter, outputNameLimit), outputTestEmptyTitle} {
		result := safeFilename(title)
		if filepath.Base(result) != result || strings.ContainsAny(result, outputTestUnsafeCharacters) || strings.HasPrefix(result, outputTestHyphen) || len(result) > outputNameLimit+len(outputExtension) {
			test.Fatalf(outputTestUnsafeFilenameFor, result, title)
		}
	}
}

func TestOverwriteFailurePreservesOriginal(test *testing.T) {
	directory := test.TempDir()

	target := filepath.Join(directory, testTitle+outputExtension)
	if err := os.WriteFile(target, []byte(testContents), testPermissions); err != nil {
		test.Fatal(err)
	}

	artifact := &fixtureArtifact{}

	failure := errors.New(outputTestFixtureFailure)
	provider := &fixtureProvider{title: testTitle}
	streams := &fixtureStreams{artifact: artifact}
	converter := &fixtureConverter{failure: failure}
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	if err := service.Video(context.Background(), testID, Options{OutputDirectory: directory, Overwrite: true}); err == nil {
		test.Fatal(outputTestFailedConversionAccepted)
	}

	body, err := os.ReadFile(target)
	if err != nil || string(body) != testContents {
		test.Fatalf(outputTestOriginalOutputChanged, body, err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || !artifact.closed {
		test.Fatalf(outputTestIncompleteCleanup, entries, err)
	}
}

type barrierConverter struct {
	arrived chan struct{}
	release chan struct{}
}

func (converter *barrierConverter) Convert(
	operationContext context.Context,
	_ progress.Logger,
	_ []string,
	result string,
) error {
	if err := os.WriteFile(result, []byte(testContents), testPermissions); err != nil {
		return err
	}

	converter.arrived <- struct{}{}

	select {
	case <-converter.release:
		return nil
	case <-operationContext.Done():
		return operationContext.Err()
	}
}

func TestConcurrentPublicationPreservesWinner(test *testing.T) {
	const (
		competingDownloads  = 2
		publicationDeadline = 5 * time.Second
	)

	operationContext, cancel := context.WithTimeout(context.Background(), publicationDeadline)
	defer cancel()

	directory := test.TempDir()
	converter := &barrierConverter{arrived: make(chan struct{}, competingDownloads), release: make(chan struct{})}
	results := make(chan error, competingDownloads)

	var workers sync.WaitGroup

	test.Cleanup(func() {
		cancel()
		workers.Wait()
	})

	artifacts := make([]*fixtureArtifact, 0, competingDownloads)
	for range competingDownloads {
		artifact := &fixtureArtifact{}
		artifacts = append(artifacts, artifact)

		provider := &fixtureProvider{title: testTitle}
		streams := &fixtureStreams{artifact: artifact}
		logger := &fixtureLogger{}

		service, err := New(provider, streams, converter, logger)
		if err != nil {
			test.Fatal(err)
		}

		workers.Go(func() {
			results <- service.Video(operationContext, testID, Options{OutputDirectory: directory})
		})
	}

	for range competingDownloads {
		select {
		case <-converter.arrived:
		case <-operationContext.Done():
			test.Fatal(outputTestConversionsFailedToReachPublication)
		}
	}

	close(converter.release)

	successes := 0

	for range competingDownloads {
		select {
		case err := <-results:
			if err == nil {
				successes++
			}
		case <-operationContext.Done():
			test.Fatal(outputTestDownloadsDidNotJoin)
		}
	}

	if successes != 1 {
		test.Fatalf(outputTestSuccessfulCompetingPublications, successes)
	}

	for _, artifact := range artifacts {
		if !artifact.closed {
			test.Fatal(outputTestArtifactRemainedOpen)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		test.Fatalf(outputTestPublicationCleanup, entries, err)
	}

	body, err := os.ReadFile(filepath.Join(directory, testTitle+outputExtension))
	if err != nil || string(body) != testContents {
		test.Fatalf(outputTestWinnerOutput, body, err)
	}
}

func TestDownloadFailureNeverConverts(test *testing.T) {
	directory := test.TempDir()
	failure := errors.New(outputTestFixtureSegmentFailure)
	converter := &fixtureConverter{}

	provider := &fixtureProvider{title: testTitle}
	streams := &fixtureStreams{failure: failure}
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	if err := service.Video(context.Background(), testID, Options{OutputDirectory: directory}); !errors.Is(err, failure) {
		test.Fatalf(outputTestError, err)
	}

	if converter.calls != 0 {
		test.Fatal(outputTestConversionStartedAfterFailedDownload)
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		test.Fatalf(outputTestPartialOutput, entries, err)
	}
}

type variantFixtureReader struct{}

func (*variantFixtureReader) Read(context.Context, string) (client.Response, error) {
	baseURL, err := url.Parse(fixtureMasterURL)
	return client.Response{Body: []byte(fixtureMasterBody), URL: baseURL}, err
}

func TestOutputFilenamePolicy(test *testing.T) {
	fixtures := []struct {
		title    string
		expected string
	}{
		{title: outputFixtureEscapeTitle, expected: outputFixtureEscapeName},
		{title: outputFixtureSeparatorTitle, expected: outputFixtureSeparatorName},
		{title: outputFixtureNewlineTitle, expected: outputFixtureNewlineName},
		{title: outputFixtureHyphenTitle, expected: outputFixtureHyphenName},
		{title: outputFixtureUnicodeTitle, expected: outputFixtureUnicodeName},
		{title: outputFixtureQuoteTitle, expected: outputFixtureQuoteName},
		{title: outputFixtureFallbackTitle, expected: outputFixtureFallbackName},
	}
	for _, fixture := range fixtures {
		if actual := safeFilename(fixture.title); actual != fixture.expected {
			test.Fatalf(outputFixtureFilenameMismatch, fixture.title, fixture.expected, actual)
		}
	}
}

func TestZeroExitWithoutUsableOutputNeverPublishes(test *testing.T) {
	for _, emptyFile := range []bool{false, true} {
		test.Run(map[bool]string{false: outputTestNoOutput, true: outputTestEmptyOutput}[emptyFile], func(test *testing.T) {
			directory := test.TempDir()
			provider := &fixtureProvider{title: testTitle}
			artifact := &fixtureArtifact{}
			streams := &fixtureStreams{artifact: artifact}
			logger := &fixtureLogger{}
			converter := conversionAdapter(func(
				_ context.Context,
				_ progress.Logger,
				_ []string,
				output string,
			) error {
				if emptyFile {
					return os.WriteFile(output, nil, testPermissions)
				}

				return nil
			})

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			if err := service.Video(test.Context(), testID, Options{OutputDirectory: directory}); err == nil {
				test.Fatalf(outputFixtureInvalidSuccess, err)
			}

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 || !artifact.closed {
				test.Fatalf(outputTestIncompleteCleanup, entries, err)
			}

			if logger.newlines != 0 || strings.Contains(strings.Join(logger.suffixes, ""), readyMessage) {
				test.Fatalf(outputFixtureUnexpectedReady, logger.suffixes)
			}
		})
	}
}

func TestOverwritePublishesOnlyCompletedOutput(test *testing.T) {
	directory := test.TempDir()

	target := filepath.Join(directory, testTitle+outputExtension)
	if err := os.WriteFile(target, []byte(outputFixtureOriginal), testPermissions); err != nil {
		test.Fatal(err)
	}

	provider := &fixtureProvider{title: testTitle}
	artifact := &fixtureArtifact{}
	streams := &fixtureStreams{artifact: artifact}
	converter := &fixtureConverter{}
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	if err := service.Video(test.Context(), testID, Options{OutputDirectory: directory, Overwrite: true}); err != nil {
		test.Fatal(err)
	}

	body, err := os.ReadFile(target)
	if err != nil || string(body) != testContents || logger.newlines != 1 {
		test.Fatalf(outputFixtureOverwriteFailure, body, err)
	}
}

func TestCleanupFailureIsNotReportedAsSuccess(test *testing.T) {
	directory := test.TempDir()
	failure := errors.New(outputFixtureCleanupFailure)
	provider := &fixtureProvider{title: testTitle}
	artifact := &fixtureArtifact{failure: failure}
	streams := &fixtureStreams{artifact: artifact}
	converter := &fixtureConverter{}
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	if err := service.Video(test.Context(), testID, Options{OutputDirectory: directory}); !errors.Is(err, failure) {
		test.Fatalf(outputFixtureCleanupErrorLost, err)
	}

	if logger.newlines != 0 || strings.Contains(strings.Join(logger.suffixes, ""), readyMessage) {
		test.Fatalf(outputFixtureUnexpectedReady, logger.suffixes)
	}
}
