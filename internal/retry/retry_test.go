package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"
)

const (
	retryFixtureErrorFormat       = "attempts=%d elapsed=%s error=%v"
	retryCanceledErrorFormat      = "expected cancellation, got %v"
	retryCanceledOperationMessage = "operation started after cancellation"
)

var errRetryFixture = io.ErrUnexpectedEOF

func TestRetryRecovers(test *testing.T) {
	const (
		expectedDelay    = time.Second
		expectedAttempts = 2
	)

	synctest.Test(test, func(test *testing.T) {
		attempts := 0
		started := time.Now()

		err := Do(context.Background(), func(context.Context) error {
			attempts++
			if attempts == 1 {
				return errRetryFixture
			}

			return nil
		})
		if err != nil || attempts != expectedAttempts || time.Since(started) != expectedDelay {
			test.Fatalf(retryFixtureErrorFormat, attempts, time.Since(started), err)
		}
	})
}

func TestRetryExhaustsAttempts(test *testing.T) {
	synctest.Test(test, func(test *testing.T) {
		const (
			expectedAttempts = 3
			expectedDelay    = 3 * time.Second
		)

		attempts := 0
		started := time.Now()

		err := Do(context.Background(), func(context.Context) error {
			attempts++
			return errRetryFixture
		})
		if !errors.Is(err, errRetryFixture) || attempts != expectedAttempts || time.Since(started) != expectedDelay {
			test.Fatalf(retryFixtureErrorFormat, attempts, time.Since(started), err)
		}
	})
}

func TestRetryStopsOnCancellation(test *testing.T) {
	synctest.Test(test, func(test *testing.T) {
		started := time.Now()

		operationContext, cancel := context.WithCancel(context.Background())
		defer cancel()

		attempts := 0

		err := Do(operationContext, func(context.Context) error {
			attempts++

			cancel()

			return errRetryFixture
		})
		if !errors.Is(err, context.Canceled) || attempts != 1 {
			test.Fatalf(retryFixtureErrorFormat, attempts, time.Since(started), err)
		}

		err = Do(operationContext, func(context.Context) error {
			test.Fatal(retryCanceledOperationMessage)
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			test.Fatalf(retryCanceledErrorFormat, err)
		}
	})
}

func TestRetryStopsOnNotRetryableError(test *testing.T) {
	const (
		wrapperFormat         = "request failed: %w"
		unexpectedRetryFormat = "attempts=%d elapsed=%s error=%v"
	)

	synctest.Test(test, func(test *testing.T) {
		attempts := 0
		started := time.Now()
		notRetryable := MarkAsNotRetryable(errRetryFixture)
		wrapped := fmt.Errorf(wrapperFormat, notRetryable)

		err := Do(context.Background(), func(context.Context) error {
			attempts++
			return wrapped
		})
		if attempts != 1 || time.Since(started) != 0 || err != wrapped || !errors.Is(err, errRetryFixture) {
			test.Fatalf(unexpectedRetryFormat, attempts, time.Since(started), err)
		}

		if err := MarkAsNotRetryable(nil); err != nil {
			test.Fatal(err)
		}
	})
}

func TestRetryDeadlineInterruptsBackoff(test *testing.T) {
	const (
		deadline              = 500 * time.Millisecond
		unexpectedRetryFormat = "attempts=%d elapsed=%s error=%v"
	)

	synctest.Test(test, func(test *testing.T) {
		operationContext, cancel := context.WithTimeout(context.Background(), deadline)
		defer cancel()

		attempts := 0
		started := time.Now()

		err := Do(operationContext, func(context.Context) error {
			attempts++
			return errRetryFixture
		})
		if attempts != 1 || time.Since(started) != deadline || !errors.Is(err, context.DeadlineExceeded) {
			test.Fatalf(unexpectedRetryFormat, attempts, time.Since(started), err)
		}
	})
}
