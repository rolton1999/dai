package hls

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestPlaylistRetryPolicy(test *testing.T) {
	const (
		malformedBody     = "not a playlist"
		encryptedBody     = "#EXTM3U\n#EXT-X-SESSION-KEY:METHOD=AES-128\n#EXT-X-KEY:METHOD=AES-128\n"
		masterCase        = "master"
		mediaCase         = "media"
		transientAttempts = 5
		transientDelay    = 10 * time.Second
		resultFormat      = "requests=%d elapsed=%s error=%v"
	)

	fixtures := []struct {
		name     string
		status   int
		body     string
		attempts int
		elapsed  time.Duration
	}{
		{name: "missing", status: http.StatusNotFound, attempts: 1},
		{name: "unavailable", status: http.StatusServiceUnavailable, attempts: transientAttempts, elapsed: transientDelay},
		{name: "malformed", status: http.StatusOK, body: malformedBody, attempts: 1},
		{name: "encrypted", status: http.StatusOK, body: encryptedBody, attempts: 1},
	}
	for _, kind := range []string{masterCase, mediaCase} {
		for _, fixture := range fixtures {
			test.Run(kind+fixture.name, func(test *testing.T) {
				synctest.Test(test, func(test *testing.T) {
					attempts := 0
					transport := masterTransport(func(request *http.Request) (*http.Response, error) {
						attempts++
						reader := strings.NewReader(fixture.body)
						body := io.NopCloser(reader)
						response := &http.Response{
							StatusCode: fixture.status,
							Body:       body,
							Request:    request,
						}

						return response, nil
					})
					originalClient := http.DefaultClient

					test.Cleanup(func() {
						http.DefaultClient = originalClient
					})

					http.DefaultClient = &http.Client{Transport: transport}
					started := time.Now()

					var err error
					if kind == masterCase {
						_, err = GetVariantPlaylists(masterFixtureURL)
					} else {
						playlist := VariantPlaylist{Link: masterFixtureURL}
						_, err = collectSegmentMetas(context.Background(), playlist, "", "")
					}

					if err == nil || attempts != fixture.attempts || time.Since(started) != fixture.elapsed {
						test.Fatalf(resultFormat, attempts, time.Since(started), err)
					}
				})
			})
		}
	}
}
