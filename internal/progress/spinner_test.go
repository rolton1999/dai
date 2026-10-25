package progress_test

import (
	"context"
	"dai/internal/progress"
	"sync"
	"testing"
	"time"
)

const (
	spinnerTestTimeout  = time.Second
	spinnerStopperCount = 8
)

const (
	spinnerTestSpinnerDidNotStopAfterCancellation = "spinner did not stop after cancellation"
)

type signalLogger struct {
	done chan struct{}
	once sync.Once
}

func (*signalLogger) NewPrefix(string) {
}

func (*signalLogger) PrintNewLine() {
}

func (logger *signalLogger) UpdateWithSuffix(string) {
	logger.once.Do(func() {
		close(logger.done)
	})
}

func TestSpinnerCancellationAndIdempotentStop(test *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	logger := &signalLogger{done: make(chan struct{})}
	stop := progress.SpinningMessageContext(operationContext, logger, "", "")

	cancel()

	timer := time.NewTimer(spinnerTestTimeout)
	defer timer.Stop()

	select {
	case <-logger.done:
	case <-timer.C:
		test.Fatal(spinnerTestSpinnerDidNotStopAfterCancellation)
	}

	var stoppers sync.WaitGroup
	for range spinnerStopperCount {
		stoppers.Go(stop)
	}

	stoppers.Wait()
	stop()
}
