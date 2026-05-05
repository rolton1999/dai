package util

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

		for frameI := 0; ; frameI = (frameI + 1) % len(frames) {
			select {
			case <-stop:
				return
			default:
			}
			logger.UpdateWithSuffix(fmt.Sprintf("%s %s", message, frames[frameI]))
			time.Sleep(200 * time.Millisecond)
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}
