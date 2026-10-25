package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
)

func RetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func RetryableError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}

	var statusError *StatusError
	if errors.As(err, &statusError) {
		return RetryableStatus(statusError.Code)
	}

	var fileError *os.PathError
	if errors.As(err, &fileError) {
		return false
	}

	var requestError *url.Error
	if errors.As(err, &requestError) {
		return RetryableError(requestError.Err)
	}

	var networkError net.Error

	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.As(err, &networkError)
}
