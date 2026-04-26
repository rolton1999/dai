package main

import (
	"fmt"
	"time"
)

func spinningMessage(message string, after string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		frames := []string{"/", "-", "\\", "|"}

		defer func() {
			// need to perfectly vanish the line by \r
			for len(after) < len(message)+5 /*5 for spin signs and `...`*/ {
				after += " "
			}
			fmt.Printf("\r%s\n", after)
			close(done)
		}()

		for frameI := 0; ; frameI = (frameI + 1) % len(frames) {
			select {
			case <-stop:
				return
			default:
			}
			fmt.Printf("\r%s %s...", frames[frameI], message)
			time.Sleep(200 * time.Millisecond)
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}
