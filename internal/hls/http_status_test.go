package hls

import (
	"context"
	"dai/internal/http/client"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	masterUnreadSuccessBodyMessage = "successful response was not read"

	masterHTTPErrorFormat     = "HTTP %d"
	masterHTTPFailureFormat   = "error=%v status=%d"
	masterHTTPLifecycleFormat = "reads=%d closed=%v"
)

type masterStatusBody struct {
	reader io.Reader
	reads  int
	closed bool
}

func (body *masterStatusBody) Read(destination []byte) (int, error) {
	body.reads++
	return body.reader.Read(destination)
}

func (body *masterStatusBody) Close() error {
	body.closed = true
	return nil
}

func TestMasterRejectsHTTPErrorBeforeParsing(test *testing.T) {
	for _, status := range []int{
		http.StatusOK,
		http.StatusNotFound,
		http.StatusServiceUnavailable,
	} {
		test.Run(http.StatusText(status), func(test *testing.T) {
			reader := strings.NewReader(masterFixtureBody)
			body := &masterStatusBody{reader: reader}
			transport := masterTransport(func(request *http.Request) (*http.Response, error) {
				response := &http.Response{
					StatusCode: status,
					Body:       body,
					Request:    request,
				}

				return response, nil
			})
			httpClient := &http.Client{Transport: transport}
			streamingClient := client.NewStreamingClient(httpClient)
			test.Cleanup(streamingClient.Close)
			playlistReader := client.New(streamingClient)

			_, err := getVariantPlaylistsWithReader(context.Background(), playlistReader, masterFixtureURL)
			if status == http.StatusOK {
				if err != nil {
					test.Fatalf(masterHTTPFailureFormat, err, status)
				}

				if body.reads == 0 {
					test.Fatal(masterUnreadSuccessBodyMessage)
				}
			} else {
				expected := fmt.Sprintf(masterHTTPErrorFormat, status)
				if err == nil || !strings.Contains(err.Error(), expected) {
					test.Fatalf(masterHTTPFailureFormat, err, status)
				}

				if body.reads != 0 {
					test.Fatalf(masterHTTPLifecycleFormat, body.reads, body.closed)
				}
			}

			if !body.closed {
				test.Fatalf(masterHTTPLifecycleFormat, body.reads, body.closed)
			}
		})
	}
}
