package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	fixtureShortBody              = "short"
	fixtureCanceledDeadline       = 100 * time.Millisecond
	fixtureCompletionDeadline     = time.Second
	fixtureHeaderStallPath        = "/headers"
	fixtureBodyStallPath          = "/body"
	fixtureRedirectPath           = "/redirect"
	fixtureFinalPath              = "/nested/final"
	fixtureLengthHeader           = "Content-Length"
	fixtureDeclaredLength         = "100"
	fixtureBoundsErrorFormat      = "response=%v error=%v closed=%v"
	fixtureMissingDeadlineMessage = "request has no bounded deadline"
	fixtureHandlerStalledMessage  = "server handler remained active after cancellation"
)

type boundsDoer struct {
	body   io.ReadCloser
	length int64
}

func (doer *boundsDoer) Do(request *http.Request) (*http.Response, error) {
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Body:          doer.body,
		ContentLength: doer.length,
		Request:       request,
	}

	return response, nil
}

func (doer *boundsDoer) Close() {
}

type boundsFailureReader struct {
	failure error
}

func (reader boundsFailureReader) Read([]byte) (int, error) {
	return 0, reader.failure
}

func TestReadClosesBodyOnTruncationAndReadFailure(test *testing.T) {
	shortReader := strings.NewReader(fixtureShortBody)

	failureReader := boundsFailureReader{failure: io.ErrUnexpectedEOF}
	for _, reader := range []io.Reader{
		shortReader,
		failureReader,
	} {
		body := &trackedBody{Reader: reader}
		doer := &boundsDoer{body: body, length: int64(len(fixtureBody))}

		client, clientConstructorError := New(doer)
		if clientConstructorError != nil {
			test.Fatal(clientConstructorError)
		}

		response, err := client.Read(context.Background(), fixtureURL)
		if !errors.Is(err, io.ErrUnexpectedEOF) || !body.closed {
			test.Fatalf(fixtureBoundsErrorFormat, response, err, body.closed)
		}
	}
}

func TestReadRejectsDeclaredOversizeWithoutReading(test *testing.T) {
	reader := strings.NewReader(fixtureBody)
	body := &trackedBody{Reader: reader}
	doer := &boundsDoer{body: body, length: MaximumBodySize + 1}

	client, clientConstructorError := New(doer)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	response, err := client.Read(context.Background(), fixtureURL)
	if err == nil || !body.closed || reader.Len() != len(fixtureBody) {
		test.Fatalf(fixtureBoundsErrorFormat, response, err, body.closed)
	}
}

func TestReadLimitsUnknownLengthBody(test *testing.T) {
	const oversizePadding = 100

	contents := strings.Repeat(fixtureBody, MaximumBodySize/len(fixtureBody)+oversizePadding)
	reader := strings.NewReader(contents)
	body := &trackedBody{Reader: reader}
	doer := &boundsDoer{body: body, length: -1}

	client, clientConstructorError := New(doer)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	response, err := client.Read(context.Background(), fixtureURL)
	if err == nil || !body.closed || reader.Len() != len(contents)-(MaximumBodySize+1) {
		test.Fatalf(fixtureBoundsErrorFormat, response, err, body.closed)
	}
}

func TestReadCancelsStalledHeadersAndBodies(test *testing.T) {
	for _, path := range []string{
		fixtureHeaderStallPath,
		fixtureBodyStallPath,
	} {
		test.Run(path, func(test *testing.T) {
			started := make(chan struct{})
			finished := make(chan struct{})
			handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(finished)

				if path == fixtureBodyStallPath {
					writer.Header().Set(fixtureLengthHeader, fixtureDeclaredLength)
					writer.WriteHeader(http.StatusOK)
					writer.(http.Flusher).Flush()
				}

				close(started)
				<-request.Context().Done()
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			transport := server.Client()

			streamingClient := NewStreamingClient(transport)
			defer streamingClient.Close()

			client, clientConstructorError := New(streamingClient)
			if clientConstructorError != nil {
				test.Fatal(clientConstructorError)
			}

			operationContext, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)

			done := make(chan struct{})
			go func() {
				defer close(done)

				_, err := client.Read(operationContext, server.URL+path)
				result <- err
			}()

			defer func() {
				cancel()

				cleanupTimer := time.NewTimer(fixtureCompletionDeadline)
				defer cleanupTimer.Stop()

				select {
				case <-done:
				case <-cleanupTimer.C:
					test.Error(fixtureHandlerStalledMessage)
				}
			}()

			deadline := time.NewTimer(fixtureCompletionDeadline)
			defer deadline.Stop()

			select {
			case <-started:
			case <-deadline.C:
				test.Fatal(fixtureHandlerStalledMessage)
			}

			cancel()

			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					test.Fatalf(fixtureBoundsErrorFormat, Response{}, err, false)
				}
			case <-deadline.C:
				test.Fatal(fixtureHandlerStalledMessage)
			}

			select {
			case <-finished:
			case <-deadline.C:
				test.Fatal(fixtureHandlerStalledMessage)
			}
		})
	}
}

func TestReadHonorsCallerDeadline(test *testing.T) {
	doer := &blockingDoer{
		started:  make(chan struct{}),
		finished: make(chan struct{}),
	}

	client, clientConstructorError := New(doer)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	operationContext, cancel := context.WithTimeout(context.Background(), fixtureCanceledDeadline)
	defer cancel()

	_, err := client.Read(operationContext, fixtureURL)
	if !errors.Is(err, context.DeadlineExceeded) {
		test.Fatalf(fixtureBoundsErrorFormat, Response{}, err, false)
	}
}

func TestReadUsesFinalRedirectURL(test *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == fixtureRedirectPath {
			http.Redirect(writer, request, fixtureFinalPath, http.StatusFound)
			return
		}

		_, _ = io.WriteString(writer, fixtureBody)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	transport := server.Client()

	streamingClient := NewStreamingClient(transport)
	defer streamingClient.Close()

	client, clientConstructorError := New(streamingClient)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	response, err := client.Read(context.Background(), server.URL+fixtureRedirectPath)
	if err != nil || response.URL == nil || response.URL.Path != fixtureFinalPath || string(response.Body) != fixtureBody {
		test.Fatalf(fixtureBoundsErrorFormat, response, err, false)
	}
}

type deadlineDoer struct {
	observed bool
}

func (doer *deadlineDoer) Do(request *http.Request) (*http.Response, error) {
	deadline, exists := request.Context().Deadline()
	remaining := time.Until(deadline)
	doer.observed = exists && remaining > 0 && remaining <= requestTimeout
	reader := strings.NewReader(fixtureBody)
	body := io.NopCloser(reader)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
		Request:    request,
	}

	return response, nil
}

func (doer *deadlineDoer) Close() {
}

func TestReadAlwaysAddsRequestDeadline(test *testing.T) {
	doer := &deadlineDoer{}

	client, clientConstructorError := New(doer)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	_, err := client.Read(context.Background(), fixtureURL)
	if err != nil || !doer.observed {
		test.Fatal(fixtureMissingDeadlineMessage, err)
	}
}

type responseErrorDoer struct {
	body io.ReadCloser
}

func (doer *responseErrorDoer) Do(request *http.Request) (*http.Response, error) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       doer.body,
		Request:    request,
	}

	return response, io.ErrUnexpectedEOF
}

func (doer *responseErrorDoer) Close() {
}

func TestReadClosesResponseReturnedWithTransportError(test *testing.T) {
	reader := strings.NewReader(fixtureBody)
	body := &trackedBody{Reader: reader}
	doer := &responseErrorDoer{body: body}

	client, clientConstructorError := New(doer)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	response, err := client.Read(context.Background(), fixtureURL)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !body.closed {
		test.Fatalf(fixtureBoundsErrorFormat, response, err, body.closed)
	}
}

func TestStreamingClientReusesAndClosesConnections(test *testing.T) {
	const (
		expectedConnections    = 1
		requestCount           = 2
		connectionReuseError   = "connections=%d, expected one reused connection"
		connectionCleanupError = "idle connection remained open"
	)

	var connections atomic.Int32

	closed := make(chan struct{}, requestCount)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, fixtureBody)
	})
	server := httptest.NewUnstartedServer(handler)
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}

		if state == http.StateClosed {
			closed <- struct{}{}
		}
	}

	server.Start()
	defer server.Close()

	httpClient := NewStreamingClient(nil)
	defer httpClient.Close()

	reader, readerConstructorError := New(httpClient)
	if readerConstructorError != nil {
		test.Fatal(readerConstructorError)
	}

	for range requestCount {
		if _, err := reader.Read(context.Background(), server.URL); err != nil {
			test.Fatal(err)
		}
	}

	if connections.Load() != expectedConnections {
		test.Fatalf(connectionReuseError, connections.Load())
	}

	httpClient.Close()

	timer := time.NewTimer(fixtureCompletionDeadline)
	defer timer.Stop()

	select {
	case <-closed:
	case <-timer.C:
		test.Fatal(connectionCleanupError)
	}
}
