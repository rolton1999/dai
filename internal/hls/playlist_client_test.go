package hls

import (
	"context"
	"dai/internal/http/client"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConstructorRejectsMissingDependencies(test *testing.T) {
	const acceptedMessage = "accepted missing HLS dependency"

	network := client.NewStreamingClient(nil)
	defer network.Close()

	reader, err := client.New(network)
	if err != nil {
		test.Fatal(err)
	}

	fixtures := []struct {
		reader  client.Client
		network client.StreamingClient
	}{
		{nil, network},
		{reader, nil},
	}
	for _, fixture := range fixtures {
		if streams, err := New(fixture.reader, fixture.network); err == nil || streams != nil {
			test.Fatal(acceptedMessage)
		}
	}
}

func TestConvenienceVariantsHonorCallerCancellation(test *testing.T) {
	operationContext, cancel := context.WithCancel(test.Context())
	cancel()

	if _, err := GetVariantPlaylists(operationContext, masterFixtureURL); !errors.Is(err, context.Canceled) {
		test.Fatalf(injectedErrorFormat, nil, err)
	}
}

const (
	injectedMediaURL        = "https://cdn.example.test/old/media.m3u8"
	injectedFinalMediaURL   = "https://cdn.example.test/path/media.m3u8"
	injectedFilePrefix      = "fixture"
	injectedRequestDeadline = time.Second
	injectedCanceledMessage = "injected playlist request failed to cancel"
	injectedErrorFormat     = "result=%v error=%v"
)

func TestInjectedMediaReaderUsesFinalURLAndClosesBody(test *testing.T) {
	finalURL, err := url.Parse(injectedFinalMediaURL)
	if err != nil {
		test.Fatal(err)
	}

	source := strings.NewReader(mediaFixtureBody)
	body := &masterStatusBody{reader: source}
	transport := masterTransport(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL = finalURL
		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Request:    redirected,
		}

		return response, nil
	})
	httpClient := &http.Client{Transport: transport}
	streamingClient := client.NewStreamingClient(httpClient)
	test.Cleanup(streamingClient.Close)

	reader, readerConstructorError := client.New(streamingClient)
	if readerConstructorError != nil {
		test.Fatal(readerConstructorError)
	}

	playlist := VariantPlaylist{Link: injectedMediaURL}

	segments, err := collectSegmentMetasOnce(context.Background(), reader, playlist, "", injectedFilePrefix)
	if err != nil || len(segments) != 1 || segments[0].link != mediaFixtureResolvedSegment || !body.closed {
		test.Fatalf(injectedErrorFormat, segments, err)
	}
}

func TestInjectedMasterReaderHonorsCancellation(test *testing.T) {
	started := make(chan struct{})
	transport := masterTransport(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()

		return nil, request.Context().Err()
	})
	httpClient := &http.Client{Transport: transport}
	streamingClient := client.NewStreamingClient(httpClient)
	test.Cleanup(streamingClient.Close)

	reader, readerConstructorError := client.New(streamingClient)
	if readerConstructorError != nil {
		test.Fatal(readerConstructorError)
	}

	client, clientConstructorError := New(reader, streamingClient)
	if clientConstructorError != nil {
		test.Fatal(clientConstructorError)
	}

	operationContext, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)

		_, err := client.Variants(operationContext, masterFixtureURL)
		result <- err
	}()

	defer func() {
		cancel()

		timer := time.NewTimer(injectedRequestDeadline)
		defer timer.Stop()

		select {
		case <-done:
		case <-timer.C:
			test.Error(injectedCanceledMessage)
		}
	}()

	timer := time.NewTimer(injectedRequestDeadline)
	defer timer.Stop()

	select {
	case <-started:
	case <-timer.C:
		test.Fatal(injectedCanceledMessage)
	}

	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			test.Fatalf(injectedErrorFormat, nil, err)
		}
	case <-timer.C:
		test.Fatal(injectedCanceledMessage)
	}
}
