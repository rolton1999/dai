package client

import (
	"dai/internal/retry"
	"fmt"
	"net/http"
	"testing"
)

func TestHTTPRetryableStatuses(test *testing.T) {
	const (
		unexpectedStatusFormat = "status=%d retry=%v"
		wrapperFormat          = "request failed: %w"
	)

	fixtures := []struct {
		status int
		retry  bool
	}{
		{status: http.StatusOK},
		{status: http.StatusMovedPermanently},
		{status: http.StatusBadRequest},
		{status: http.StatusUnauthorized},
		{status: http.StatusForbidden},
		{status: http.StatusNotFound},
		{status: http.StatusRequestTimeout, retry: true},
		{status: http.StatusTooManyRequests, retry: true},
		{status: http.StatusInternalServerError, retry: true},
		{status: http.StatusNotImplemented},
		{status: http.StatusBadGateway, retry: true},
		{status: http.StatusServiceUnavailable, retry: true},
		{status: http.StatusGatewayTimeout, retry: true},
	}
	for _, fixture := range fixtures {
		statusError := &StatusError{Code: fixture.status}

		wrappedError := fmt.Errorf(wrapperFormat, statusError)
		for _, failure := range []error{statusError, wrappedError} {
			if shouldRetry := retry.IsRetryable(failure); shouldRetry != fixture.retry {
				test.Errorf(unexpectedStatusFormat, fixture.status, shouldRetry)
			}
		}
	}
}
