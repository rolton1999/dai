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
	network := &http.Client{Transport: transport}
	reader := client.New(network)
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
	network := &http.Client{Transport: transport}
	reader := client.New(network)
	client := NewPlaylistClient(reader)
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
