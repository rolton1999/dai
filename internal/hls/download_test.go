package hls_test

import (
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"dai/internal/hls"
)

const (
	fixtureOrigin                = "https://cdn.example.test"
	fixturePlaylistPath          = "/media/video.m3u8"
	fixtureFirstSegmentPath      = "/media/segment-1.ts"
	fixtureSecondSegmentPath     = "/media/segment-2.ts"
	fixtureFirstSegmentBody      = "first segment"
	fixtureSecondSegmentBody     = "second segment"
	fixtureOutputName            = "video.mp4"
	fixtureTemporaryDirectoryKey = "TMPDIR"
	fixtureMediaPlaylist         = "#EXTM3U\n#EXT-X-TARGETDURATION:7\n#EXT-X-ALLOW-CACHE:YES\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-VERSION:3\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:3.937,\nsegment-1.ts\n#EXTINF:3.937,\nsegment-2.ts\n#EXT-X-ENDLIST\n"
	fixtureMinimalPlaylist       = "#EXTM3U\n#EXTINF:3.937,\nsegment-1.ts\n#EXTINF:3.937,\nsegment-2.ts\n#EXT-X-ENDLIST\n"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (transport fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type silentLogger struct{}

func (silentLogger) NewPrefix(string) {}

func (silentLogger) UpdateWithSuffix(string) {}

func (silentLogger) PrintNewLine() {}

type interruptedSegmentReader struct{}

func (interruptedSegmentReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestDownloadTSFilesRequestsOnlySegments(test *testing.T) {
	fixtures := []struct {
		name     string
		playlist string
	}{
		{name: "trailing newline", playlist: fixtureMediaPlaylist},
		{name: "no trailing newline", playlist: strings.TrimSpace(fixtureMediaPlaylist)},
		{name: "CRLF", playlist: strings.ReplaceAll(fixtureMediaPlaylist, "\n", "\r\n")},
		{name: "minimal headers", playlist: fixtureMinimalPlaylist},
		{name: "absolute segment URLs", playlist: strings.ReplaceAll(fixtureMinimalPlaylist, "segment-", fixtureOrigin+"/media/segment-")},
		{name: "root relative segment URLs", playlist: strings.ReplaceAll(fixtureMinimalPlaylist, "segment-", "/media/segment-")},
	}

	for _, fixture := range fixtures {
		test.Run(fixture.name, func(test *testing.T) {
			unexpectedRequests := setFixtureClient(test, fixture.playlist, http.StatusOK)

			files, err := hls.DownloadTSFiles(silentLogger{}, fixtureOutputName, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
			if err != nil {
				test.Fatalf("download failed: %v", err)
			}

			expectedBodies := []string{fixtureFirstSegmentBody, fixtureSecondSegmentBody}
			if len(files) != len(expectedBodies) {
				test.Fatalf("expected %d segments, got %d", len(expectedBodies), len(files))
			}

			for segmentIndex, fileName := range files {
				body, err := os.ReadFile(fileName)
				if err != nil {
					test.Fatal(err)
				}

				if string(body) != expectedBodies[segmentIndex] {
					test.Errorf("segment %d: expected %q, got %q", segmentIndex, expectedBodies[segmentIndex], body)
				}
			}

			if unexpectedRequests.Load() != 0 {
				test.Fatal("requested a URL that was not a segment")
			}
		})
	}
}

func TestDownloadTSFilesRejectsRealSegmentFailure(test *testing.T) {
	setFixtureClient(test, fixtureMediaPlaylist, http.StatusBadGateway)

	files, err := hls.DownloadTSFiles(silentLogger{}, fixtureOutputName, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
	if err == nil || files != nil {
		test.Fatalf("expected a failed segment to abort download, got files %v and error %v", files, err)
	}

	artifacts, err := os.ReadDir(os.Getenv(fixtureTemporaryDirectoryKey))
	if err != nil || len(artifacts) != 0 {
		test.Fatalf("failed download left temporary artifacts: %v, error %v", artifacts, err)
	}
}

func TestDownloadTSFilesRecoversInterruptedSegment(test *testing.T) {
	const partialBody = "partial body longer than the complete first segment"

	setFixtureClient(test, fixtureMediaPlaylist, http.StatusOK)

	transport := http.DefaultClient.Transport
	attempts := atomic.Int32{}
	http.DefaultClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == fixtureFirstSegmentPath && attempts.Add(1) == 1 {
			partialReader := strings.NewReader(partialBody)
			body := io.MultiReader(partialReader, interruptedSegmentReader{})

			responseBody := io.NopCloser(body)
			response := &http.Response{
				StatusCode: http.StatusOK,
				Body:       responseBody,
				Request:    request,
			}

			return response, nil
		}

		return transport.RoundTrip(request)
	})

	files, err := hls.DownloadTSFiles(silentLogger{}, fixtureOutputName, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
	if err != nil {
		test.Fatal(err)
	}

	expectedBodies := []string{fixtureFirstSegmentBody, fixtureSecondSegmentBody}
	if len(files) != len(expectedBodies) {
		test.Fatalf("expected %d complete segments, got %d", len(expectedBodies), len(files))
	}

	for segmentIndex, fileName := range files {
		body, err := os.ReadFile(fileName)
		if err != nil || string(body) != expectedBodies[segmentIndex] {
			test.Fatalf("segment %d retained partial data: %q, error %v", segmentIndex, body, err)
		}
	}

	if attempts.Load() != 2 {
		test.Fatalf("expected two attempts for interrupted segment, got %d", attempts.Load())
	}
}

func TestDownloadTSFilesRejectsMalformedPlaylist(test *testing.T) {
	const (
		emptyPlaylist     = "#EXTM3U\n"
		truncatedPlaylist = "#EXTM3U\n#EXTINF:3.937,\n"
		invalidPlaylist   = "not a playlist"
	)

	for _, playlist := range []string{emptyPlaylist, truncatedPlaylist, invalidPlaylist} {
		test.Run(playlist, func(test *testing.T) {
			setFixtureClient(test, playlist, http.StatusOK)

			files, err := hls.DownloadTSFiles(silentLogger{}, fixtureOutputName, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
			if err == nil || files != nil {
				test.Fatalf("expected an invalid playlist error, got files %v and error %v", files, err)
			}
		})
	}
}

func setFixtureClient(test *testing.T, playlist string, segmentStatus int) *atomic.Int32 {
	test.Helper()
	test.Setenv(fixtureTemporaryDirectoryKey, test.TempDir())

	originalClient := http.DefaultClient

	test.Cleanup(func() { http.DefaultClient = originalClient })

	unexpectedRequests := &atomic.Int32{}
	transport := fixtureTransport(func(request *http.Request) (*http.Response, error) {
		statusCode := http.StatusOK

		var body string

		switch request.URL.Path {
		case fixturePlaylistPath:
			body = playlist
		case fixtureFirstSegmentPath:
			body = fixtureFirstSegmentBody
			statusCode = segmentStatus
		case fixtureSecondSegmentPath:
			body = fixtureSecondSegmentBody
			statusCode = segmentStatus
		default:
			unexpectedRequests.Add(1)

			statusCode = http.StatusBadGateway
		}

		bodyReader := strings.NewReader(body)

		responseBody := io.NopCloser(bodyReader)
		response := &http.Response{
			StatusCode: statusCode,
			Body:       responseBody,
			Request:    request,
		}

		return response, nil
	})

	http.DefaultClient = &http.Client{Transport: transport}

	return unexpectedRequests
}

func TestDownloadTSFilesRejectsEncryptionBeforeSegmentRequests(test *testing.T) {
	const (
		header        = "#EXTM3U\n"
		encryptionTag = "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n"
	)

	playlist := strings.Replace(fixtureMinimalPlaylist, header, header+encryptionTag, 1)
	setFixtureClient(test, playlist, http.StatusOK)

	originalTransport := http.DefaultClient.Transport

	var segmentRequests atomic.Int32

	http.DefaultClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != fixturePlaylistPath {
			segmentRequests.Add(1)
		}

		return originalTransport.RoundTrip(request)
	})

	files, err := hls.DownloadTSFiles(silentLogger{}, fixtureOutputName, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
	if err == nil || files != nil || segmentRequests.Load() != 0 {
		test.Fatalf("files=%v error=%v segment requests=%d", files, err, segmentRequests.Load())
	}

	artifacts, err := os.ReadDir(os.Getenv(fixtureTemporaryDirectoryKey))
	if err != nil || len(artifacts) != 0 {
		test.Fatalf("temporary artifacts=%v error=%v", artifacts, err)
	}
}
