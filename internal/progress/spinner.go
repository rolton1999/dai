package progress

import (
	"fmt"
	"time"
)

func SpinningMessage(logger Logger, message string, after string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		frames := []string{"/", "-", "\\", "|"}

		defer func() {
			logger.UpdateWithSuffix(after)
			close(done)
		}()

		const tickTime = 200 * time.Millisecond

		ticker := time.NewTicker(tickTime)

		for frameI := 0; ; frameI = (frameI + 1) % len(frames) {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}

			logger.UpdateWithSuffix(fmt.Sprintf("%s %s", message, frames[frameI]))
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}
