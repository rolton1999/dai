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

func (silentLogger) NewPrefix(string)        {}
func (silentLogger) UpdateWithSuffix(string) {}
func (silentLogger) PrintNewLine()           {}

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
	http.DefaultClient = &http.Client{Transport: fixtureTransport(func(request *http.Request) (*http.Response, error) {
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

		return &http.Response{StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}

	return unexpectedRequests
}
