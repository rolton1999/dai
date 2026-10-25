package hls_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"dai/internal/hls"
)

const retryMasterBody = "#EXTM3U\n#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=1000,FRAME-RATE=30,CODECS=\"avc1.4d400c,mp4a.40.2\",RESOLUTION=1280x720\nhttps://example.test/media.m3u8\n"

func TestMasterPlaylistRetriesInterruptedBody(test *testing.T) {
	synctest.Test(test, func(test *testing.T) {
		originalClient := http.DefaultClient

		test.Cleanup(func() {
			http.DefaultClient = originalClient
		})

		attempts := 0
		transport := fixtureTransport(func(request *http.Request) (*http.Response, error) {
			attempts++

			var body io.Reader = strings.NewReader(retryMasterBody)
			if attempts == 1 {
				body = interruptedSegmentReader{}
			}

			responseBody := io.NopCloser(body)
			response := &http.Response{
				StatusCode: http.StatusOK,
				Body:       responseBody,
				Request:    request,
			}

			return response, nil
		})

		http.DefaultClient = &http.Client{Transport: transport}

		playlists, err := hls.GetVariantPlaylists(fixtureOrigin)
		if err != nil || len(playlists) != 1 || attempts != 2 {
			test.Fatalf("playlists=%v attempts=%d error=%v", playlists, attempts, err)
		}
	})
}

func TestMediaPlaylistRetriesInterruptedBody(test *testing.T) {
	synctest.Test(test, func(test *testing.T) {
		setFixtureClient(test, fixtureMediaPlaylist, http.StatusOK)

		transport := http.DefaultClient.Transport
		attempts := 0
		http.DefaultClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == fixturePlaylistPath {
				attempts++
				if attempts == 1 {
					responseBody := io.NopCloser(interruptedSegmentReader{})
					response := &http.Response{
						StatusCode: http.StatusOK,
						Body:       responseBody,
						Request:    request,
					}

					return response, nil
				}
			}

			return transport.RoundTrip(request)
		})

		files, err := hls.DownloadTSFiles(silentLogger{}, fixtureOutputName, hls.VariantPlaylist{Link: fixtureOrigin + fixturePlaylistPath})
		if err != nil || attempts != 2 || len(files) == 0 {
			test.Fatalf("files=%v attempts=%d error=%v", files, attempts, err)
		}
	})
}
