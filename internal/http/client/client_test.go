package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	oversizedFixtureByte           = "x"
	readResponseErrorFormat        = "response=%q error=%v"
	readUnexpectedErrorFormat      = "unexpected error: %v"
	readLifecycleErrorFormat       = "closed=%v calls=%d"
	readOversizeAcceptedMessage    = "accepted oversized response"
	readCallsErrorFormat           = "error=%v calls=%d"
	readRequestStartMessage        = "request did not start"
	readCancellationStalledMessage = "cancellation stalled"
	readRequestActiveMessage       = "request remained active"
)

const fixtureURL = "https://example.test/metadata"
const fixtureBody = "metadata"

type responseDoer struct {
	status int
	body   *trackedBody
	calls  int
}

func (doer *responseDoer) Do(request *http.Request) (*http.Response, error) {
	doer.calls++
	return &http.Response{StatusCode: doer.status, Body: doer.body, Request: request, ContentLength: int64(len(fixtureBody))}, nil
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (body *trackedBody) Close() error {
	body.closed = true
	return nil
}

func TestReadClosesResponse(test *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		test.Run(http.StatusText(status), func(test *testing.T) {
			reader := strings.NewReader(fixtureBody)
			body := &trackedBody{Reader: reader}
			doer := &responseDoer{status: status, body: body}

			client := New(doer)

			response, err := client.Read(context.Background(), fixtureURL)
			if status == http.StatusOK {
				if err != nil || string(response.Body) != fixtureBody {
					test.Fatalf(readResponseErrorFormat, response.Body, err)
				}
			} else {
				var statusError *StatusError
				if !errors.As(err, &statusError) || statusError.Code != status {
					test.Fatalf(readUnexpectedErrorFormat, err)
				}
			}

			if !body.closed || doer.calls != 1 {
				test.Fatalf(readLifecycleErrorFormat, body.closed, doer.calls)
			}
		})
	}
}

func TestReadRejectsOversizedResponse(test *testing.T) {
	contents := strings.Repeat(oversizedFixtureByte, MaximumBodySize+1)
	reader := strings.NewReader(contents)
	body := &trackedBody{Reader: reader}

	doer := &responseDoer{status: http.StatusOK, body: body}

	client := New(doer)
	if _, err := client.Read(context.Background(), fixtureURL); err == nil {
		test.Fatal(readOversizeAcceptedMessage)
	}

	if !body.closed || doer.calls != 1 {
		test.Fatalf(readLifecycleErrorFormat, body.closed, doer.calls)
	}
}

func TestReadCanceledBeforeRequest(test *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	cancel()

	doer := &responseDoer{}

	client := New(doer)

	_, err := client.Read(operationContext, fixtureURL)
	if !errors.Is(err, context.Canceled) || doer.calls != 0 {
		test.Fatalf(readCallsErrorFormat, err, doer.calls)
	}
}

type blockingDoer struct {
	started  chan struct{}
	finished chan struct{}
}

func (doer *blockingDoer) Do(request *http.Request) (*http.Response, error) {
	close(doer.started)
	<-request.Context().Done()
	close(doer.finished)

	return nil, request.Context().Err()
}

func TestReadCancelsStalledRequest(test *testing.T) {
	const cancellationDeadline = time.Second

	doer := &blockingDoer{started: make(chan struct{}), finished: make(chan struct{})}

	operationContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)

	go func() {
		client := New(doer)

		_, err := client.Read(operationContext, fixtureURL)
		result <- err
	}()

	timer := time.NewTimer(cancellationDeadline)
	defer timer.Stop()

	select {
	case <-doer.started:
	case <-timer.C:
		test.Fatal(readRequestStartMessage)
	}

	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			test.Fatalf(readUnexpectedErrorFormat, err)
		}
	case <-timer.C:
		test.Fatal(readCancellationStalledMessage)
	}

	select {
	case <-doer.finished:
	default:
		test.Fatal(readRequestActiveMessage)
	}
}
