package download

import (
	"context"
	"dai/internal/ffmpeg"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dai/internal/hls"
	"dai/internal/http/client"
	"dai/internal/progress"
	"dai/internal/rutube"
)

func TestPlaylistFailureQuotesRemoteTitle(test *testing.T) {
	const (
		remoteTitle               = "title\x1b[2J\x1b[H\nforged message"
		quotedTitleFormat         = "%q"
		terminalControlCharacters = "\x1b\n"
		unexpectedTitle           = "remote title was not safely quoted: %q"
	)

	provider := &parallelProvider{identifiers: []string{remoteTitle}}
	streams := &parallelStreams{}
	converter := conversionAdapter(func(context.Context, progress.Logger, ffmpeg.Input, string) error {
		return errors.New(parallelFailureMessage)
	})
	logger := &fixtureLogger{}

	service, err := New(provider, streams, converter, logger)
	if err != nil {
		test.Fatal(err)
	}

	directory := test.TempDir()

	err = service.Playlist(test.Context(), parallelPlaylistID, Options{Jobs: 1, OutputDirectory: directory})
	if err == nil || strings.ContainsAny(err.Error(), terminalControlCharacters) || !strings.Contains(err.Error(), fmt.Sprintf(quotedTitleFormat, remoteTitle)) {
		test.Fatalf(unexpectedTitle, err)
	}
}

const (
	parallelItemCount         = 16
	parallelWorkerCount       = 4
	parallelDeadline          = 5 * time.Second
	parallelIDFormat          = "video-%02d"
	parallelFailureMessage    = "playlist conversion failed"
	parallelUnexpectedMaximum = "maximum active videos: %d; expected %d"
	parallelUnexpectedResult  = "playlist result: %v"
	parallelUnexpectedOutput  = "output %s: %q, %v"
	parallelUnexpectedCleanup = "closed=%d started=%d active=%d"
	parallelUnexpectedStarts  = "unexpected started video: %s"
	parallelDidNotFinish      = "playlist did not finish before deadline"
	parallelDidNotStart       = "video did not start before deadline"
	parallelUnexpectedFiles   = "unexpected output files: %v, %v"
	parallelJobsCaseFormat    = "jobs-%d"
	parallelPlaylistID        = "playlist"
)

type parallelProvider struct {
	identifiers []string
}

func (provider *parallelProvider) Video(_ context.Context, identifier string) (rutube.VideoInfo, error) {
	return rutube.VideoInfo{Title: identifier, MasterPlaylistLink: identifier}, nil
}

func (provider *parallelProvider) Playlist(context.Context, string) (rutube.PlaylistInfo, error) {
	return rutube.PlaylistInfo{VideoIDs: provider.identifiers}, nil
}

type parallelStreams struct {
	active  atomic.Int32
	maximum atomic.Int32
	started atomic.Int32
	closed  atomic.Int32
}

func (*parallelStreams) Variants(operationContext context.Context, identifier string) ([]hls.VariantPlaylist, error) {
	reader := &variantFixtureReader{}

	network := client.NewStreamingClient(nil)
	defer network.Close()

	streams, streamsConstructorError := hls.New(reader, network)
	if streamsConstructorError != nil {
		return nil, streamsConstructorError
	}

	variants, err := streams.Variants(operationContext, fixtureMasterURL)
	if err != nil {
		return nil, err
	}

	variants[0].Link = identifier

	return variants, nil
}

func (streams *parallelStreams) Download(_ context.Context, logger progress.Logger, playlist hls.VariantPlaylist) (hls.Artifact, error) {
	active := streams.active.Add(1)
	streams.started.Add(1)

	for {
		maximum := streams.maximum.Load()
		if active <= maximum || streams.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}

	logger.UpdateWithSuffix(loadingTestProgress)

	return &parallelArtifact{streams: streams, identifier: playlist.Link}, nil
}

const loadingTestProgress = " ~ loading 100%"

type parallelArtifact struct {
	streams    *parallelStreams
	identifier string
}

func (artifact *parallelArtifact) Files() []string {
	return []string{artifact.identifier}
}

func (*parallelArtifact) Duration() float64 {
	return fixtureDurationSeconds
}

func (artifact *parallelArtifact) Close() error {
	artifact.streams.closed.Add(1)
	artifact.streams.active.Add(-1)

	return nil
}

type parallelConverter struct {
	started  chan string
	releases map[string]chan error
}

func (converter *parallelConverter) Convert(
	operationContext context.Context,
	logger progress.Logger,
	input ffmpeg.Input,
	output string,
) error {
	identifier := input.Files[0]

	logger.UpdateWithSuffix(parallelConverting)

	if err := os.WriteFile(output, []byte(identifier), testPermissions); err != nil {
		return err
	}

	select {
	case converter.started <- identifier:
	case <-operationContext.Done():
		return operationContext.Err()
	}

	select {
	case err := <-converter.releases[identifier]:
		return err
	case <-operationContext.Done():
		return operationContext.Err()
	}
}

const parallelConverting = " ~ converting"

func newParallelFixture(test *testing.T, count int) (*parallelProvider, *parallelStreams, *parallelConverter) {
	test.Helper()

	provider := &parallelProvider{identifiers: make([]string, count)}
	streams := &parallelStreams{}

	converter := &parallelConverter{started: make(chan string, count), releases: make(map[string]chan error)}
	for index := range count {
		identifier := fmt.Sprintf(parallelIDFormat, index)
		provider.identifiers[index] = identifier
		converter.releases[identifier] = make(chan error, 1)
	}

	return provider, streams, converter
}

func waitParallelStart(test *testing.T, operationContext context.Context, started <-chan string) string {
	test.Helper()

	select {
	case identifier := <-started:
		return identifier
	case <-operationContext.Done():
		test.Fatal(parallelDidNotStart)
		return ""
	}
}

func waitParallelResult(test *testing.T, operationContext context.Context, results <-chan error) error {
	test.Helper()

	select {
	case err := <-results:
		return err
	case <-operationContext.Done():
		test.Fatal(parallelDidNotFinish)
		return nil
	}
}

func TestPlaylistLimitsConcurrencyAndRefillsCompletedSlots(test *testing.T) {
	for _, jobs := range []int{0, 1, parallelWorkerCount, parallelItemCount + 1} {
		test.Run(fmt.Sprintf(parallelJobsCaseFormat, jobs), func(test *testing.T) {
			operationContext, cancel := context.WithTimeout(test.Context(), parallelDeadline)
			defer cancel()

			provider, streams, converter := newParallelFixture(test, parallelItemCount)
			logger := &fixtureLogger{}

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			directory := test.TempDir()
			results := make(chan error, 1)

			var workers sync.WaitGroup

			test.Cleanup(func() {
				cancel()
				workers.Wait()
			})
			workers.Go(func() {
				results <- service.Playlist(operationContext, parallelPlaylistID, Options{OutputDirectory: directory, Jobs: jobs})
			})

			expected := jobs
			if expected == 0 {
				expected = DefaultJobs
			}

			expected = min(expected, parallelItemCount)
			first := ""

			for range expected {
				identifier := waitParallelStart(test, operationContext, converter.started)
				if first == "" {
					first = identifier
				}
			}

			select {
			case identifier := <-converter.started:
				test.Fatalf(parallelUnexpectedStarts, identifier)
			default:
			}

			converter.releases[first] <- nil

			if expected < parallelItemCount {
				waitParallelStart(test, operationContext, converter.started)
			}

			for identifier, release := range converter.releases {
				if identifier != first {
					release <- nil
				}
			}

			if err := waitParallelResult(test, operationContext, results); err != nil {
				test.Fatalf(parallelUnexpectedResult, err)
			}

			if maximum := streams.maximum.Load(); maximum != int32(expected) {
				test.Fatalf(parallelUnexpectedMaximum, maximum, expected)
			}

			if streams.closed.Load() != parallelItemCount || streams.active.Load() != 0 {
				test.Fatalf(parallelUnexpectedCleanup, streams.closed.Load(), streams.started.Load(), streams.active.Load())
			}

			for _, identifier := range provider.identifiers {
				body, err := os.ReadFile(filepath.Join(directory, identifier+outputExtension))
				if err != nil || string(body) != identifier {
					test.Fatalf(parallelUnexpectedOutput, identifier, body, err)
				}
			}
		})
	}
}

func TestPlaylistFailureCancelsAndJoinsActiveVideos(test *testing.T) {
	for _, canceled := range []bool{false, true} {
		test.Run(fmt.Sprint(canceled), func(test *testing.T) {
			operationContext, cancel := context.WithTimeout(test.Context(), parallelDeadline)
			defer cancel()

			provider, streams, converter := newParallelFixture(test, parallelItemCount)
			logger := &fixtureLogger{}

			service, err := New(provider, streams, converter, logger)
			if err != nil {
				test.Fatal(err)
			}

			directory := test.TempDir()
			results := make(chan error, 1)

			var workers sync.WaitGroup

			test.Cleanup(func() {
				cancel()
				workers.Wait()
			})
			workers.Go(func() {
				results <- service.Playlist(operationContext, parallelPlaylistID, Options{OutputDirectory: directory, Jobs: parallelWorkerCount})
			})

			completed := waitParallelStart(test, operationContext, converter.started)

			failing := waitParallelStart(test, operationContext, converter.started)
			for range parallelWorkerCount - 2 {
				waitParallelStart(test, operationContext, converter.started)
			}

			converter.releases[completed] <- nil

			waitParallelStart(test, operationContext, converter.started)

			failure := errors.New(parallelFailureMessage)

			if canceled {
				cancel()

				failure = context.Canceled
			} else {
				converter.releases[failing] <- failure
			}

			// Joining is bounded independently of the canceled operation.
			joinContext, stop := context.WithTimeout(test.Context(), parallelDeadline)
			defer stop()

			if err := waitParallelResult(test, joinContext, results); !errors.Is(err, failure) {
				test.Fatalf(parallelUnexpectedResult, err)
			}

			if streams.started.Load() != parallelWorkerCount+1 || streams.closed.Load() != streams.started.Load() || streams.active.Load() != 0 {
				test.Fatalf(parallelUnexpectedCleanup, streams.closed.Load(), streams.started.Load(), streams.active.Load())
			}

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != completed+outputExtension {
				test.Fatalf(parallelUnexpectedFiles, entries, err)
			}
		})
	}
}
