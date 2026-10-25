package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
)

type retryableError interface {
	error
	Retryable() bool
}

func IsRetryable(err error) bool {
	var notRetryable *notRetryableError
	if errors.As(err, &notRetryable) {
		return false
	}

	if errors.Is(err, context.Canceled) {
		return false
	}

	var fileError *os.PathError
	if errors.As(err, &fileError) {
		return false
	}

	var decision retryableError
	if errors.As(err, &decision) {
		return decision.Retryable()
	}

	var requestError *url.Error
	if errors.As(err, &requestError) {
		return IsRetryable(requestError.Err)
	}

	var networkError net.Error

	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.As(err, &networkError)
}

func MarkAsNotRetryable(err error) error {
	if err == nil {
		return nil
	}

	return &notRetryableError{cause: err}
}

type notRetryableError struct {
	cause error
}

func (failure *notRetryableError) Error() string {
	return failure.cause.Error()
}

func (failure *notRetryableError) Unwrap() error {
	return failure.cause
}
