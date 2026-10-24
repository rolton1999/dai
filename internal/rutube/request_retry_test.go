package rutube

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

const (
	retryVideoID            = "video-id"
	retryVideoBody          = `{"title":"video","video_balancer":{"m3u8":"https://example.test/master.m3u8"}}`
	retryPlaylistID         = "playlist-id"
	retryPlaylistTitlePath  = "/api/playlist/custom/playlist-id/"
	retryPlaylistVideosPath = "/api/playlist/custom/playlist-id/videos/"
	retryPlaylistTitleBody  = `{"title":"playlist"}`
	retryPlaylistFirstPage  = `{"results":[{"id":"first"}],"has_next":true,"next":"https://rutube.ru/page-two"}`
	retryPlaylistSecondPath = "/page-two"
	retryPlaylistSecondPage = `{"results":[{"id":"second"}],"has_next":false}`
	retryTimeoutMessage     = "TLS handshake timeout"
)

type retryTransport func(*http.Request) (*http.Response, error)

func (transport retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestVideoInfoRetriesInterruptedResponse(test *testing.T) {
	originalClient := http.DefaultClient

	test.Cleanup(func() {
		http.DefaultClient = originalClient
	})

	attempts := 0
	transport := retryTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, &net.DNSError{Err: retryTimeoutMessage, IsTimeout: true}
		}

		bodyReader := strings.NewReader(retryVideoBody)

		responseBody := io.NopCloser(bodyReader)
		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       responseBody,
			Request:    request,
		}

		return response, nil
	})

	http.DefaultClient = &http.Client{Transport: transport}

	info, err := GetVideoInfo(retryVideoID)
	if err != nil || attempts != 2 || info.MasterPlaylistLink == "" {
		test.Fatalf("info=%v attempts=%d error=%v", info, attempts, err)
	}
}

func TestPlaylistRetryDoesNotDuplicateEarlierPages(test *testing.T) {
	const expectedVideos = 2

	originalClient := http.DefaultClient

	test.Cleanup(func() {
		http.DefaultClient = originalClient
	})

	secondPageAttempts := 0
	transport := retryTransport(func(request *http.Request) (*http.Response, error) {
		var body string

		switch request.URL.Path {
		case retryPlaylistTitlePath:
			body = retryPlaylistTitleBody
		case retryPlaylistVideosPath:
			body = retryPlaylistFirstPage
		case retryPlaylistSecondPath:
			secondPageAttempts++
			if secondPageAttempts == 1 {
				return nil, &net.DNSError{Err: retryTimeoutMessage, IsTimeout: true}
			}

			body = retryPlaylistSecondPage
		default:
			test.Fatalf("unexpected request: %s", request.URL.Path)
		}

		bodyReader := strings.NewReader(body)

		responseBody := io.NopCloser(bodyReader)
		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       responseBody,
			Request:    request,
		}

		return response, nil
	})

	http.DefaultClient = &http.Client{Transport: transport}

	info, err := GetPlaylistInfo(retryPlaylistID)
	if err != nil || secondPageAttempts != 2 || len(info.VideoIDs) != expectedVideos {
		test.Fatalf("info=%v attempts=%d error=%v", info, secondPageAttempts, err)
	}

	if info.VideoIDs[0] == info.VideoIDs[1] {
		test.Fatalf("retry duplicated playlist entries: %v", info.VideoIDs)
	}
}
