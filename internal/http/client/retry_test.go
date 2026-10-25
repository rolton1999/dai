package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
)

func TestHTTPRetryableStatuses(test *testing.T) {
	const unexpectedStatusFormat = "status=%d retry=%v"

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
		if retry := RetryableStatus(fixture.status); retry != fixture.retry {
			test.Errorf(unexpectedStatusFormat, fixture.status, retry)
		}
	}
}

func TestHTTPRetryableErrors(test *testing.T) {
	const (
		unknownErrorMessage   = "fixture read failure"
		networkTimeoutMessage = "fixture network timeout"
		wrapperFormat         = "read response: %w"
		requestOperation      = "Get"
		requestURL            = "https://example.test/playlist"
		fileOperation         = "write"
		filePath              = "segment.ts"
		unexpectedRetryFormat = "error=%v retry=%v"
	)

	unknownError := errors.New(unknownErrorMessage)
	networkError := &net.DNSError{Err: networkTimeoutMessage, IsTimeout: true}
	wrappedError := fmt.Errorf(wrapperFormat, networkError)
	fileError := &os.PathError{
		Op:   fileOperation,
		Path: filePath,
		Err:  networkError,
	}
	permanentRequestError := &url.Error{
		Op:  requestOperation,
		URL: requestURL,
		Err: unknownError,
	}
	transientRequestError := &url.Error{
		Op:  requestOperation,
		URL: requestURL,
		Err: networkError,
	}

	missingStatus := &StatusError{Code: http.StatusNotFound}
	unavailableStatus := &StatusError{Code: http.StatusServiceUnavailable}
	wrappedStatus := fmt.Errorf(wrapperFormat, unavailableStatus)

	fixtures := []struct {
		failure error
		retry   bool
	}{
		{failure: context.Canceled},
		{failure: context.DeadlineExceeded, retry: true},
		{failure: io.ErrUnexpectedEOF, retry: true},
		{failure: io.EOF, retry: true},
		{failure: networkError, retry: true},
		{failure: wrappedError, retry: true},
		{failure: unknownError},
		{failure: missingStatus},
		{failure: unavailableStatus, retry: true},
		{failure: wrappedStatus, retry: true},
		{failure: permanentRequestError},
		{failure: transientRequestError, retry: true},
		{failure: fileError},
		{},
	}
	for _, fixture := range fixtures {
		if retry := RetryableError(fixture.failure); retry != fixture.retry {
			test.Errorf(unexpectedRetryFormat, fixture.failure, retry)
		}
	}
}
