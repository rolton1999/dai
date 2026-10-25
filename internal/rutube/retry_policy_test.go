package rutube

import (
	"context"
	"dai/internal/http/client"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const (
	policyVideoPath         = "/api/play/options/video-id/"
	policyMalformedJSON     = `{invalid`
	policyWrongJSONType     = `[]`
	policyTransientAttempts = 5
	policyTransientDelay    = 10 * time.Second
	policyResultFormat      = "requests=%d elapsed=%s error=%v"
)

func TestMetadataRetryPolicy(test *testing.T) {
	fixtures := []struct {
		name     string
		status   int
		body     string
		attempts int
		elapsed  time.Duration
	}{
		{name: "missing", status: http.StatusNotFound, attempts: 1},
		{name: "unauthorized", status: http.StatusUnauthorized, attempts: 1},
		{name: "unavailable", status: http.StatusServiceUnavailable, attempts: policyTransientAttempts, elapsed: policyTransientDelay},
		{name: "malformed", status: http.StatusOK, body: policyMalformedJSON, attempts: 1},
		{name: "wrong type", status: http.StatusOK, body: policyWrongJSONType, attempts: 1},
	}
	for _, endpoint := range []string{policyVideoPath, retryPlaylistTitlePath, retryPlaylistVideosPath, retryPlaylistSecondPath} {
		for _, fixture := range fixtures {
			test.Run(endpoint+fixture.name, func(test *testing.T) {
				synctest.Test(test, func(test *testing.T) {
					attempts := 0
					transport := retryTransport(func(request *http.Request) (*http.Response, error) {
						status := http.StatusOK
						body := retryPlaylistTitleBody

						switch request.URL.Path {
						case retryPlaylistVideosPath:
							body = retryPlaylistFirstPage
						case retryPlaylistSecondPath:
							body = retryPlaylistSecondPage
						}

						if request.URL.Path == endpoint {
							attempts++
							status = fixture.status
							body = fixture.body
						}

						reader := strings.NewReader(body)
						responseBody := io.NopCloser(reader)
						response := &http.Response{
							StatusCode: status,
							Body:       responseBody,
							Request:    request,
						}

						return response, nil
					})
					httpClient := &http.Client{Transport: transport}
					streamingClient := client.NewStreamingClient(httpClient)
					test.Cleanup(streamingClient.Close)
					reader := client.New(streamingClient)
					provider := newMetadataProvider(reader)

					started := time.Now()

					var err error
					if endpoint == policyVideoPath {
						_, err = provider.Video(context.Background(), retryVideoID)
					} else {
						_, err = provider.Playlist(context.Background(), retryPlaylistID)
					}

					if err == nil || attempts != fixture.attempts || time.Since(started) != fixture.elapsed {
						test.Fatalf(policyResultFormat, attempts, time.Since(started), err)
					}

					if fixture.status != http.StatusOK {
						expected := fmt.Sprintf(statusFailureFormat, fixture.status)
						if !strings.Contains(err.Error(), expected) {
							test.Fatal(err)
						}
					}
				})
			})
		}
	}
}
