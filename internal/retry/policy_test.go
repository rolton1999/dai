package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"testing"
)

func TestRetryableTransferErrors(test *testing.T) {
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
		{failure: permanentRequestError},
		{failure: transientRequestError, retry: true},
		{failure: fileError},
		{},
	}
	for _, fixture := range fixtures {
		if retry := IsRetryable(fixture.failure); retry != fixture.retry {
			test.Errorf(unexpectedRetryFormat, fixture.failure, retry)
		}
	}
}

func TestRetryStopsOnUnclassifiedError(test *testing.T) {
	const (
		failureMessage = "invalid response"
		resultFormat   = "attempts=%d error=%v"
	)

	failure := errors.New(failureMessage)
	attempts := 0

	err := Do(context.Background(), func(context.Context) error {
		attempts++
		return failure
	})
	if attempts != 1 || err != failure {
		test.Fatalf(resultFormat, attempts, err)
	}
}
