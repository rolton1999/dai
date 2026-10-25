package progress

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	spinnerFrames   = "/-\\|"
	spinnerInterval = 200 * time.Millisecond
	spinnerFormat   = "%s %c"
)

func SpinningMessageContext(
	operationContext context.Context,
	logger Logger,
	message string,
	after string,
) func() {
	stop := make(chan struct{})
	done := make(chan struct{})

	var once sync.Once

	go func() {
		defer close(done)
		defer logger.UpdateWithSuffix(after)

		ticker := time.NewTicker(spinnerInterval)
		defer ticker.Stop()

		for frameIndex := 0; ; frameIndex = (frameIndex + 1) % len(spinnerFrames) {
			select {
			case <-operationContext.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				logger.UpdateWithSuffix(fmt.Sprintf(spinnerFormat, message, spinnerFrames[frameIndex]))
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(stop)
		})
		<-done
	}
}
