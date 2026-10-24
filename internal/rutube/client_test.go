package rutube

import (
	"context"
	"dai/internal/http/client"
	"errors"
	"fmt"
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
	for _, playlist := range []bool{false, true} {
		test.Run(fmt.Sprint(playlist), func(test *testing.T) {
			started := make(chan struct{})
			transport := retryTransport(func(request *http.Request) (*http.Response, error) {
				close(started)
				<-request.Context().Done()

				return nil, request.Context().Err()
			})
			network := &http.Client{Transport: transport}
			reader := client.New(network)
			provider := New(reader)
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
	provider := New(reader)

	info, err := provider.Playlist(context.Background(), retryPlaylistID)
	if err != nil || len(info.VideoIDs) != expectedVideos || info.VideoIDs[0] != metadataFirstID {
		test.Fatalf(clientRelativeErrorFormat, info, err)
	}
}
