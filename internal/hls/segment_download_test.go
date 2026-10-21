package hls

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	segmentTestURL          = "https://cdn.example.test/segment.ts"
	segmentTestFileName     = "segment.ts"
	segmentTestPartialBody  = "partial payload longer than the final successful body"
	segmentTestCompleteBody = "complete segment"
)

type segmentTestTransport func(*http.Request) (*http.Response, error)

func (transport segmentTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type segmentTestReadFailure struct{}

func (segmentTestReadFailure) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

type segmentTestBody struct {
	io.Reader
	closed *int
}

func (body segmentTestBody) Close() error {
	*body.closed++
	return nil
}

func TestSegmentDownloadRetriesTransientFailures(test *testing.T) {
	for _, failure := range []string{"unexpected EOF", "HTTP 502", "short content length"} {
		test.Run(failure, func(test *testing.T) {
			attempts := 0
			closedBodies := 0

			var delays []time.Duration

			meta := segmentMeta{link: segmentTestURL, fileName: filepath.Join(test.TempDir(), segmentTestFileName)}
			client := &http.Client{Transport: segmentTestTransport(func(request *http.Request) (*http.Response, error) {
				attempts++
				response := &http.Response{StatusCode: http.StatusOK, Request: request}

				var reader io.Reader = strings.NewReader(segmentTestCompleteBody)

				if attempts == 1 {
					switch failure {
					case "unexpected EOF":
						reader = io.MultiReader(strings.NewReader(segmentTestPartialBody), segmentTestReadFailure{})
					case "HTTP 502":
						response.StatusCode = http.StatusBadGateway
					case "short content length":
						response.ContentLength = int64(len(segmentTestCompleteBody) + 1)
					}
				}

				response.Body = segmentTestBody{Reader: reader, closed: &closedBodies}

				return response, nil
			})}
			wait := func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			}

			if err := downloadSegment(context.Background(), client, meta, wait); err != nil {
				test.Fatal(err)
			}

			body, err := os.ReadFile(meta.fileName)
			if err != nil || string(body) != segmentTestCompleteBody {
				test.Fatalf("partial bytes survived retry: body %q, error %v", body, err)
			}

			if attempts != 2 || closedBodies != attempts || len(delays) != 1 || delays[0] != segmentRetryDelay {
				test.Fatalf("attempts=%d closed=%d delays=%v", attempts, closedBodies, delays)
			}
		})
	}
}

func TestSegmentDownloadExhaustsRetriesAndRemovesPartialFile(test *testing.T) {
	attempts := 0

	var delays []time.Duration

	meta := segmentMeta{link: segmentTestURL, fileName: filepath.Join(test.TempDir(), segmentTestFileName)}
	client := &http.Client{Transport: segmentTestTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(strings.NewReader(segmentTestPartialBody), segmentTestReadFailure{})), Request: request}, nil
	})}
	wait := func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	err := downloadSegment(context.Background(), client, meta, wait)
	if !errors.Is(err, io.ErrUnexpectedEOF) || attempts != segmentDownloadAttempts {
		test.Fatalf("expected exhausted EOF retries, attempts=%d error=%v", attempts, err)
	}

	if len(delays) != segmentDownloadAttempts-1 {
		test.Fatalf("unexpected retry delays: %v", delays)
	}

	for attempt, delay := range delays {
		if delay != segmentRetryDelay*time.Duration(1<<attempt) {
			test.Errorf("unexpected delay %s at attempt %d", delay, attempt)
		}
	}

	if _, err := os.Stat(meta.fileName); !errors.Is(err, os.ErrNotExist) {
		test.Fatalf("partial file remains: %v", err)
	}
}

func TestSegmentDownloadDoesNotRetryPermanentFailures(test *testing.T) {
	for _, failure := range []string{"HTTP 404", "file creation"} {
		test.Run(failure, func(test *testing.T) {
			attempts := 0
			meta := segmentMeta{link: segmentTestURL, fileName: filepath.Join(test.TempDir(), segmentTestFileName)}

			statusCode := http.StatusNotFound
			if failure == "file creation" {
				statusCode = http.StatusOK
				meta.fileName = filepath.Join(meta.fileName, segmentTestFileName)
			}

			client := &http.Client{Transport: segmentTestTransport(func(request *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(segmentTestCompleteBody)), Request: request}, nil
			})}
			wait := func(context.Context, time.Duration) error {
				test.Fatal("permanent failure scheduled a retry")
				return nil
			}

			if err := downloadSegment(context.Background(), client, meta, wait); err == nil || attempts != 1 {
				test.Fatalf("expected one failed attempt, got %d and error %v", attempts, err)
			}
		})
	}
}

func TestSegmentDownloadCancelsDuringBackoff(test *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0
	meta := segmentMeta{link: segmentTestURL, fileName: filepath.Join(test.TempDir(), segmentTestFileName)}
	client := &http.Client{Transport: segmentTestTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	wait := func(operationContext context.Context, delay time.Duration) error {
		cancel()
		return waitForSegmentRetry(operationContext, delay)
	}

	if err := downloadSegment(operationContext, client, meta, wait); !errors.Is(err, context.Canceled) || attempts != 1 {
		test.Fatalf("expected cancellation after one attempt, got %d and error %v", attempts, err)
	}
}
