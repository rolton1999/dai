package progress

import (
	"context"
	"time"
)

func Retry(operationContext context.Context, operation func(context.Context) error) error {
	const (
		retries        = 3
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
