package client

import (
	"net/http"
	"time"
)

const handshakeTimeout = 30 * time.Second

// StreamingClient returns responses with streaming bodies. Callers must close each body.
type StreamingClient interface {
	Do(*http.Request) (*http.Response, error)
	Close()
}

func NewStreamingClient(httpClient *http.Client) StreamingClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	client := *httpClient
	if client.Timeout <= 0 || client.Timeout > requestTimeout {
		client.Timeout = requestTimeout
	}

	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	if configurable, valid := transport.(*http.Transport); valid {
		cloned := configurable.Clone()
		cloned.TLSHandshakeTimeout = handshakeTimeout
		transport = cloned
	}

	client.Transport = transport

	return &streamingClient{httpClient: &client}
}

type streamingClient struct {
	httpClient *http.Client
}

func (client *streamingClient) Do(request *http.Request) (*http.Response, error) {
	return client.httpClient.Do(request)
}

// Close releases idle connections after the owner has finished all requests.
func (client *streamingClient) Close() {
	client.httpClient.CloseIdleConnections()
}
