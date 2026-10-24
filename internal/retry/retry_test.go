package retry

import (
	"context"
	"errors"
	"testing"
)

var errRetryFixture = errors.New("retry fixture failure")

func TestRetryRecovers(test *testing.T) {
	attempts := 0

	err := Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errRetryFixture
		}

		return nil
	})
	if err != nil || attempts != 2 {
		test.Fatalf("attempts=%d error=%v", attempts, err)
	}
}

func TestRetryExhaustsAttempts(test *testing.T) {
	const expectedAttempts = 3

	attempts := 0

	err := Do(context.Background(), func(context.Context) error {
		attempts++
		return errRetryFixture
	})
	if !errors.Is(err, errRetryFixture) || attempts != expectedAttempts {
		test.Fatalf("attempts=%d error=%v", attempts, err)
	}
}

func TestRetryStopsOnCancellation(test *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0

	err := Do(operationContext, func(context.Context) error {
		attempts++

		cancel()

		return errRetryFixture
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		test.Fatalf("attempts=%d error=%v", attempts, err)
	}

	err = Do(operationContext, func(context.Context) error {
		test.Fatal("operation started after cancellation")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		test.Fatalf("expected cancellation, got %v", err)
	}
}
