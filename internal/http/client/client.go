package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	httpScheme           = "http"
	httpSecureScheme     = "https"
	MaximumBodySize      = 8 * 1024 * 1024
	requestTimeout       = 45 * time.Second
	invalidURLError      = "expected an absolute HTTP(S) URL"
	oversizedBodyError   = "metadata response exceeds size limit"
	statusErrorFormat    = "HTTP %d"
	missingResponseError = "HTTP client returned an empty response"
)

type Response struct {
	Body []byte

	// URL is the final response URL after redirects, used to resolve relative
	// playlist entries and pagination links against the location that served them.
	URL *url.URL
}

type Client interface {
	Read(context.Context, string) (Response, error)
}

func New(httpClient StreamingClient) Client {
	return &clientImpl{httpClient: httpClient}
}

type clientImpl struct {
	httpClient StreamingClient
}

func (client *clientImpl) Read(operationContext context.Context, link string) (Response, error) {
	if err := operationContext.Err(); err != nil {
		return Response{}, err
	}

	parsed, err := url.Parse(link)
	if err != nil || (parsed.Scheme != httpScheme && parsed.Scheme != httpSecureScheme) || parsed.Hostname() == "" || parsed.User != nil {
		return Response{}, errors.New(invalidURLError)
	}

	attemptContext, cancel := context.WithTimeout(operationContext, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(attemptContext, http.MethodGet, link, nil)
	if err != nil {
		return Response{}, err
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}

		return Response{}, err
	}

	if response == nil || response.Body == nil {
		return Response{}, errors.New(missingResponseError)
	}

	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return Response{}, &StatusError{Code: response.StatusCode}
	}

	if response.ContentLength > MaximumBodySize {
		return Response{}, errors.New(oversizedBodyError)
	}

	limitedReader := io.LimitReader(response.Body, MaximumBodySize+1)

	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return Response{}, err
	}

	if len(body) > MaximumBodySize {
		return Response{}, errors.New(oversizedBodyError)
	}

	if response.ContentLength > 0 && int64(len(body)) != response.ContentLength {
		return Response{}, io.ErrUnexpectedEOF
	}

	finalURL := request.URL
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}

	return Response{
		Body: body,
		URL:  finalURL,
	}, nil
}

type StatusError struct {
	Code int
}

func (status *StatusError) Error() string {
	return fmt.Sprintf(statusErrorFormat, status.Code)
}

func (status *StatusError) Retryable() bool {
	switch status.Code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
