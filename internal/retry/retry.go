package retry

import (
	"context"
	"time"
)

func Do(operationContext context.Context, operation func(context.Context) error) error {
	const (
		retries        = 5
		backoffTimeout = time.Second
	)

	for attemptIndex := range retries {
		if err := operationContext.Err(); err != nil {
			return err
		}

		err := operation(operationContext)
		if err == nil {
			return nil
		}

		if operationContext.Err() != nil {
			return operationContext.Err()
		}

		if !IsRetryable(err) {
			return err
		}

		if attemptIndex == retries-1 {
			return err
		}

		timer := time.NewTimer(backoffTimeout * time.Duration(attemptIndex+1))
		select {
		case <-operationContext.Done():
			timer.Stop()
			return operationContext.Err()
		case <-timer.C:
		}
	}

	return nil
}
