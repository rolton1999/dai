package hls_test

import (
	"context"
	"dai/internal/http/client"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dai/internal/hls"
)

const (
	singleDurationSeconds       = 1
	multipleDurationSeconds     = 7.874
	durationComparisonTolerance = 0.000001
	unexpectedDurationFormat    = "duration=%v expected=%v"
	lifecycleDeadline           = 5 * time.Second
	singleSegmentPlaylist       = "#EXTM3U\n#EXTINF:1,\nsegment-1.ts\n#EXT-X-ENDLIST\n"
	halfProgress                = " ~ loading 50%"
	fullProgress                = " ~ loading 100%"
	unrelatedFilename           = "keep.ts"
	unrelatedContents           = "user data"
	lifecyclePermissions        = 0o600
	unexpectedProgressFormat    = "progress=%v expected=%v"
	unexpectedSegmentsFormat    = "segments=%v error=%v"
	unexpectedSegmentFormat     = "segment %d=%q error=%v"
	artifactStillExistsFormat   = "artifact directory still exists: %v"
	unrelatedChangedFormat      = "unrelated file=%q error=%v"
	artifactMutatedMessage      = "caller changed the artifact's file list"
)

type recordingLogger struct {
	suffixes        []string
	firstCompletion chan struct{}
}

func (*recordingLogger) NewPrefix(string) {
}

func (*recordingLogger) PrintNewLine() {
}

func (logger *recordingLogger) UpdateWithSuffix(suffix string) {
	logger.suffixes = append(logger.suffixes, suffix)
	if len(logger.suffixes) == 1 && logger.firstCompletion != nil {
		close(logger.firstCompletion)
	}
}

func TestArtifactOrderingProgressAndCleanup(test *testing.T) {
	fixtures := []struct {
		name     string
		playlist string
		bodies   []string
		progress []string
		duration float64
	}{
		{name: "single segment", playlist: singleSegmentPlaylist, bodies: []string{fixtureFirstSegmentBody}, progress: []string{fullProgress}, duration: singleDurationSeconds},
		{name: "reverse completion", playlist: fixtureMinimalPlaylist, bodies: []string{fixtureFirstSegmentBody, fixtureSecondSegmentBody}, progress: []string{halfProgress, fullProgress}, duration: multipleDurationSeconds},
	}
	for _, fixture := range fixtures {
		test.Run(fixture.name, func(test *testing.T) {
			setFixtureClient(test, fixture.playlist, http.StatusOK)

			unrelatedPath := filepath.Join(os.Getenv(fixtureTemporaryDirectoryKey), unrelatedFilename)
			if err := os.WriteFile(unrelatedPath, []byte(unrelatedContents), lifecyclePermissions); err != nil {
				test.Fatal(err)
			}

			firstCompletion := make(chan struct{})
			logger := &recordingLogger{firstCompletion: firstCompletion}
			transport := http.DefaultClient.Transport
			http.DefaultClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
				if fixture.playlist == fixtureMinimalPlaylist && request.URL.Path == fixtureFirstSegmentPath {
					select {
					case <-firstCompletion:
					case <-request.Context().Done():
						return nil, request.Context().Err()
					}
				}

				return transport.RoundTrip(request)
			})

			operationContext, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
			defer cancel()

			artifact, err := hls.DownloadTSFiles(operationContext, logger, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
			if err != nil {
				test.Fatal(err)
			}

			test.Cleanup(func() {
				if err := artifact.Close(); err != nil {
					test.Error(err)
				}
			})

			if math.Abs(artifact.Duration()-fixture.duration) > durationComparisonTolerance {
				test.Fatalf(unexpectedDurationFormat, artifact.Duration(), fixture.duration)
			}

			files := artifact.Files()
			if len(files) != len(fixture.bodies) {
				test.Fatalf(unexpectedSegmentsFormat, files, err)
			}

			for index, filename := range files {
				body, err := os.ReadFile(filename)
				if err != nil || string(body) != fixture.bodies[index] {
					test.Fatalf(unexpectedSegmentFormat, index, body, err)
				}
			}

			if !reflect.DeepEqual(logger.suffixes, fixture.progress) {
				test.Fatalf(unexpectedProgressFormat, logger.suffixes, fixture.progress)
			}

			directory := filepath.Dir(files[0])

			files[0] = unrelatedPath
			if artifact.Files()[0] == unrelatedPath {
				test.Fatal(artifactMutatedMessage)
			}

			if err := artifact.Close(); err != nil {
				test.Fatal(err)
			}

			if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf(artifactStillExistsFormat, err)
			}

			body, err := os.ReadFile(unrelatedPath)
			if err != nil || string(body) != unrelatedContents {
				test.Fatalf(unrelatedChangedFormat, body, err)
			}
		})
	}
}

func TestCanceledDownloadJoinsRequestBeforeCleanup(test *testing.T) {
	const requestRunningMessage = "canceled request did not finish before cleanup"

	setFixtureClient(test, singleSegmentPlaylist, http.StatusOK)

	operationContext, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
	defer cancel()

	stopped := make(chan struct{})
	transport := http.DefaultClient.Transport
	http.DefaultClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == fixtureFirstSegmentPath {
			defer close(stopped)

			cancel()
			<-request.Context().Done()

			return nil, request.Context().Err()
		}

		return transport.RoundTrip(request)
	})

	artifact, err := hls.DownloadTSFiles(operationContext, silentLogger{}, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
	if artifact != nil || !errors.Is(err, context.Canceled) {
		test.Fatalf(unexpectedSegmentsFormat, artifact, err)
	}

	select {
	case <-stopped:
	default:
		test.Fatal(requestRunningMessage)
	}

	entries, err := os.ReadDir(os.Getenv(fixtureTemporaryDirectoryKey))
	if err != nil || len(entries) != 0 {
		test.Fatalf(unexpectedSegmentsFormat, entries, err)
	}
}

type blockedSegmentBody struct {
	partialSent   bool
	started       chan struct{}
	canceled      <-chan struct{}
	closed        chan struct{}
	temporaryRoot string
	fileExisted   bool
}

func (body *blockedSegmentBody) Read(destination []byte) (int, error) {
	if !body.partialSent {
		body.partialSent = true
		return copy(destination, fixtureSecondSegmentBody), nil
	}

	close(body.started)
	<-body.canceled

	directories, err := os.ReadDir(body.temporaryRoot)
	if err == nil && len(directories) == 1 {
		directory := filepath.Join(body.temporaryRoot, directories[0].Name())

		files, err := os.ReadDir(directory)
		if err == nil && len(files) == 1 {
			filename := filepath.Join(directory, files[0].Name())
			contents, err := os.ReadFile(filename)
			body.fileExisted = err == nil && string(contents) == fixtureSecondSegmentBody
		}
	}

	return 0, context.Canceled
}

func (body *blockedSegmentBody) Close() error {
	close(body.closed)
	return nil
}

func TestFailedDownloadJoinsPartialWriterBeforeRemovingDirectory(test *testing.T) {
	const (
		prematureCleanupMessage = "temporary file removed while its writer was active"
		bodyOpenMessage         = "response body still open after download returned"
	)

	setFixtureClient(test, fixtureMinimalPlaylist, http.StatusOK)

	operationContext, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
	defer cancel()

	started := make(chan struct{})
	closed := make(chan struct{})
	body := &blockedSegmentBody{
		started:       started,
		closed:        closed,
		temporaryRoot: os.Getenv(fixtureTemporaryDirectoryKey),
	}
	transport := http.DefaultClient.Transport
	http.DefaultClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case fixtureFirstSegmentPath:
			select {
			case <-started:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}

			reader := strings.NewReader("")
			responseBody := io.NopCloser(reader)
			response := &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       responseBody,
				Request:    request,
			}

			return response, nil
		case fixtureSecondSegmentPath:
			body.canceled = request.Context().Done()
			response := &http.Response{
				StatusCode: http.StatusOK,
				Body:       body,
				Request:    request,
			}

			return response, nil
		default:
			return transport.RoundTrip(request)
		}
	})

	artifact, err := hls.DownloadTSFiles(operationContext, silentLogger{}, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})

	var statusError *client.StatusError
	if artifact != nil || !errors.As(err, &statusError) || statusError.Code != http.StatusNotFound {
		test.Fatalf(unexpectedSegmentsFormat, artifact, err)
	}

	if !body.fileExisted {
		test.Fatal(prematureCleanupMessage)
	}

	select {
	case <-closed:
	default:
		test.Fatal(bodyOpenMessage)
	}

	entries, err := os.ReadDir(os.Getenv(fixtureTemporaryDirectoryKey))
	if err != nil || len(entries) != 0 {
		test.Fatalf(unexpectedSegmentsFormat, entries, err)
	}
}
