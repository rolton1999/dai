package util

import (
	"fmt"
	"time"
)

type Logger interface {
	NewPrefix(prefix string)
	UpdateWithSuffix(suffix string)
	PrintNewLine()
}

func NewLogger() Logger {
	return &loggerImpl{}
}

type loggerImpl struct {
	longestLineLen int
	prefix         string
}

func (l *loggerImpl) NewPrefix(prefix string) {
	l.prefix = prefix
	l.longestLineLen = 0
}

func (l *loggerImpl) UpdateWithSuffix(suffix string) {
	s := fmt.Sprintf("%s%s", l.prefix, suffix)
	l.longestLineLen = max(l.longestLineLen, len(s))
	for len(s) < l.longestLineLen {
		s += " "
	}
	fmt.Printf("\r%s", s)
}

func (l *loggerImpl) PrintNewLine() {
	fmt.Println()
}

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
