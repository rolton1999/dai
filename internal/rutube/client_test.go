package rutube

import (
	"context"
	"dai/internal/http/client"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	clientTestDeadline            = time.Second
	clientCanceledMessage         = "injected request failed to cancel"
	clientCancellationErrorFormat = "error=%v"
	clientCycleMode               = "cycle"
	clientSizeMode                = "size"
	clientPageMode                = "pages"
	clientNextPageTemplate        = `{"results":[{"id":"first"}],"has_next":true,"next":"?page=%d"}`
	clientCyclePage               = `{"results":[{"id":"first"}],"has_next":true,"next":"https://rutube.ru/api/playlist/custom/playlist-id/videos/?format=json"}`
	clientPaginationErrorFormat   = "calls=%d error=%v"
	clientRedirectURL             = "https://rutube.ru/redirected/videos/?page=1"
	clientRelativePage            = `{"results":[{"id":"first"}],"has_next":true,"next":"?page=2"}`
	clientRedirectedPath          = "/redirected/videos/"
	clientRelativeErrorFormat     = "info=%v error=%v"
)

type paginationReader struct {
	calls int
	mode  string
}

func (reader *paginationReader) Read(operationContext context.Context, link string) (client.Response, error) {
	if err := operationContext.Err(); err != nil {
		return client.Response{}, err
	}

	reader.calls++

	parsed, err := url.Parse(link)
	if err != nil {
		return client.Response{}, err
	}

	body := retryPlaylistTitleBody

	if parsed.Path == retryPlaylistVideosPath {
		if reader.mode == clientCycleMode {
			body = clientCyclePage
		} else {
			body = fmt.Sprintf(clientNextPageTemplate, reader.calls)
			if reader.mode == clientSizeMode {
				body += strings.Repeat(" ", client.MaximumBodySize-len(body))
			}
		}
	}

	return client.Response{Body: []byte(body), URL: parsed}, nil
}

func TestPlaylistPaginationIsBounded(test *testing.T) {
	const (
		cycleRequests = 2
		sizeRequests  = 5
	)

	fixtures := []struct {
		mode     string
		expected int
	}{
		{clientCycleMode, cycleRequests},
		{clientSizeMode, sizeRequests},
		{clientPageMode, playlistPageLimit + 1},
	}
	for _, fixture := range fixtures {
		test.Run(fixture.mode, func(test *testing.T) {
			reader := &paginationReader{mode: fixture.mode}

			_, err := getPlaylistInfoWithReader(context.Background(), reader, retryPlaylistID)
			if err == nil || reader.calls != fixture.expected {
				test.Fatalf(clientPaginationErrorFormat, reader.calls, err)
			}
		})
	}
}

func TestInjectedProviderHonorsCancellation(test *testing.T) {
	for _, playlist := range []bool{
		false,
		true,
	} {
		test.Run(fmt.Sprint(playlist), func(test *testing.T) {
			started := make(chan struct{})
			transport := retryTransport(func(request *http.Request) (*http.Response, error) {
				close(started)
				<-request.Context().Done()

				return nil, request.Context().Err()
			})
			httpClient := &http.Client{Transport: transport}
			streamingClient := client.NewStreamingClient(httpClient)
			test.Cleanup(streamingClient.Close)
			reader := client.New(streamingClient)
			provider := newMetadataProvider(reader)
			operationContext, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)

			done := make(chan struct{})
			go func() {
				defer close(done)

				var err error
				if playlist {
					_, err = provider.Playlist(operationContext, retryPlaylistID)
				} else {
					_, err = provider.Video(operationContext, retryVideoID)
				}

				result <- err
			}()

			defer func() {
				cancel()

				timer := time.NewTimer(clientTestDeadline)
				defer timer.Stop()

				select {
				case <-done:
				case <-timer.C:
					test.Error(clientCanceledMessage)
				}
			}()

			timer := time.NewTimer(clientTestDeadline)
			defer timer.Stop()

			select {
			case <-started:
			case <-timer.C:
				test.Fatal(clientCanceledMessage)
			}

			cancel()

			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					test.Fatalf(clientCancellationErrorFormat, err)
				}
			case <-timer.C:
				test.Fatal(clientCanceledMessage)
			}
		})
	}
}

type relativePageReader struct{}

func (reader *relativePageReader) Read(_ context.Context, link string) (client.Response, error) {
	parsed, err := url.Parse(link)
	if err != nil {
		return client.Response{}, err
	}

	body := retryPlaylistTitleBody

	switch parsed.Path {
	case retryPlaylistVideosPath:
		parsed, err = url.Parse(clientRedirectURL)
		if err != nil {
			return client.Response{}, err
		}

		body = clientRelativePage
	case clientRedirectedPath:
		body = retryPlaylistSecondPage
	}

	return client.Response{Body: []byte(body), URL: parsed}, nil
}

func TestInjectedPlaylistResolvesRelativeNextAfterRedirect(test *testing.T) {
	const expectedVideos = 2

	reader := &relativePageReader{}
	provider := newMetadataProvider(reader)

	info, err := provider.Playlist(context.Background(), retryPlaylistID)
	if err != nil || len(info.VideoIDs) != expectedVideos || info.VideoIDs[0] != metadataFirstID {
		test.Fatalf(clientRelativeErrorFormat, info, err)
	}
}

type ownedMetadataTransport struct {
	closed bool
}

func (transport *ownedMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	contents := retryVideoBody

	switch request.URL.Path {
	case retryPlaylistTitlePath:
		contents = retryPlaylistTitleBody
	case retryPlaylistVideosPath:
		contents = metadataEmptyPage
	}

	reader := strings.NewReader(contents)
	body := io.NopCloser(reader)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
		Request:    request,
	}

	return response, nil
}

func (transport *ownedMetadataTransport) CloseIdleConnections() {
	transport.closed = true
}

func TestMetadataOperationsCloseIdleConnections(test *testing.T) {
	const cleanupError = "metadata operation did not close idle connections"

	for _, playlist := range []bool{false, true} {
		test.Run(fmt.Sprint(playlist), func(test *testing.T) {
			transport := &ownedMetadataTransport{}
			originalClient := http.DefaultClient

			test.Cleanup(func() {
				http.DefaultClient = originalClient
			})

			http.DefaultClient = &http.Client{Transport: transport}

			var err error
			if playlist {
				_, err = GetPlaylistInfo(retryPlaylistID)
			} else {
				_, err = GetVideoInfo(retryVideoID)
			}

			if err != nil {
				test.Fatal(err)
			}

			if !transport.closed {
				test.Fatal(cleanupError)
			}
		})
	}
}
