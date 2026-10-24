package rutube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	statusMissingMetadataMessage        = "missing valid metadata"
	statusUnrequestedEndpointMessage    = "endpoint was not requested"
	statusUnclosedBodyMessage           = "response body was not closed"
	statusMissingPlaylistSuccessMessage = "missing playlist reported success"
	statusUnreadSuccessBodyMessage      = "successful response was not read"

	statusFailureFormat         = "HTTP %d"
	statusUnexpectedErrorFormat = "error=%v, status=%d"
	statusBodyLifecycleFormat   = "reads=%d closed=%v"
	statusMissingPlaylistBody   = `{"detail":"Not found"}`
	statusMalformedBody         = `{invalid JSON`
)

type statusResponseBody struct {
	reader io.Reader
	reads  int
	closed bool
}

func (body *statusResponseBody) Read(destination []byte) (int, error) {
	body.reads++
	return body.reader.Read(destination)
}

func (body *statusResponseBody) Close() error {
	body.closed = true
	return nil
}

func newStatusBody(contents string) *statusResponseBody {
	reader := strings.NewReader(contents)
	return &statusResponseBody{reader: reader}
}

func installStatusTransport(test *testing.T, transport http.RoundTripper) {
	test.Helper()

	originalClient := http.DefaultClient

	test.Cleanup(func() {
		http.DefaultClient = originalClient
	})

	http.DefaultClient = &http.Client{Transport: transport}
}

func TestVideoRejectsHTTPErrorBeforeParsing(test *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusServiceUnavailable} {
		test.Run(http.StatusText(status), func(test *testing.T) {
			body := newStatusBody(retryVideoBody)
			transport := retryTransport(func(request *http.Request) (*http.Response, error) {
				response := &http.Response{
					StatusCode: status,
					Body:       body,
					Request:    request,
				}

				return response, nil
			})
			installStatusTransport(test, transport)

			info, err := getVideoInfo(context.Background(), retryVideoID)
			verifyStatusBody(test, status, body, err)

			if status == http.StatusOK && info.MasterPlaylistLink == "" {
				test.Fatal(statusMissingMetadataMessage)
			}
		})
	}
}

func TestPlaylistRejectsHTTPErrorAtEveryEndpoint(test *testing.T) {
	for _, endpoint := range []string{retryPlaylistTitlePath, retryPlaylistVideosPath, retryPlaylistSecondPath} {
		test.Run(endpoint, func(test *testing.T) {
			for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusServiceUnavailable} {
				test.Run(http.StatusText(status), func(test *testing.T) {
					bodies := make([]*statusResponseBody, 0)

					var checkedBody *statusResponseBody

					transport := retryTransport(func(request *http.Request) (*http.Response, error) {
						contents := retryPlaylistTitleBody

						switch request.URL.Path {
						case retryPlaylistVideosPath:
							contents = retryPlaylistFirstPage
						case retryPlaylistSecondPath:
							contents = retryPlaylistSecondPage
						}

						body := newStatusBody(contents)
						bodies = append(bodies, body)

						responseStatus := http.StatusOK
						if request.URL.Path == endpoint {
							responseStatus = status
							checkedBody = body
						}

						response := &http.Response{
							StatusCode: responseStatus,
							Body:       body,
							Request:    request,
						}

						return response, nil
					})
					installStatusTransport(test, transport)

					_, err := getPlaylistInfo(context.Background(), retryPlaylistID)

					if checkedBody == nil {
						test.Fatal(statusUnrequestedEndpointMessage)
					}

					verifyStatusBody(test, status, checkedBody, err)

					for _, body := range bodies {
						if !body.closed {
							test.Fatal(statusUnclosedBodyMessage)
						}
					}
				})
			}
		})
	}
}

func TestMissingPlaylistCannotSucceed(test *testing.T) {
	bodies := make([]*statusResponseBody, 0)
	transport := retryTransport(func(request *http.Request) (*http.Response, error) {
		body := newStatusBody(statusMissingPlaylistBody)
		bodies = append(bodies, body)
		response := &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       body,
			Request:    request,
		}

		return response, nil
	})
	installStatusTransport(test, transport)

	_, err := GetPlaylistInfo(retryPlaylistID)
	if err == nil {
		test.Fatal(statusMissingPlaylistSuccessMessage)
	}

	for _, body := range bodies {
		verifyStatusBody(test, http.StatusNotFound, body, err)
	}
}

func TestVideoClosesMalformedSuccessBody(test *testing.T) {
	body := newStatusBody(statusMalformedBody)
	transport := retryTransport(func(request *http.Request) (*http.Response, error) {
		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Request:    request,
		}

		return response, nil
	})
	installStatusTransport(test, transport)

	_, err := getVideoInfo(context.Background(), retryVideoID)
	if err == nil || !body.closed {
		test.Fatalf(statusBodyLifecycleFormat, body.reads, body.closed)
	}
}

func verifyStatusBody(
	test *testing.T,
	status int,
	body *statusResponseBody,
	err error,
) {
	test.Helper()

	if status == http.StatusOK {
		if err != nil {
			test.Fatalf(statusUnexpectedErrorFormat, err, status)
		}

		if body.reads == 0 {
			test.Fatal(statusUnreadSuccessBodyMessage)
		}
	} else {
		expected := fmt.Sprintf(statusFailureFormat, status)
		if err == nil || !strings.Contains(err.Error(), expected) {
			test.Fatalf(statusUnexpectedErrorFormat, err, status)
		}

		if body.reads != 0 {
			test.Fatalf(statusBodyLifecycleFormat, body.reads, body.closed)
		}
	}

	if !body.closed {
		test.Fatalf(statusBodyLifecycleFormat, body.reads, body.closed)
	}
}
