package pipeline_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"dai/internal/pipeline"
)

const (
	pipelineTestDeadline    = 5 * time.Second
	stageFailureMessage     = "stage failed"
	unexpectedOutputMessage = "failed stage emitted output"
	unexpectedErrorFormat   = "error=%v"
	workersRunningMessage   = "workers still running"
	cancellationItemCount   = 2
)

var errFixtureStage = errors.New(stageFailureMessage)

func TestPipelinePropagatesProducerFailure(test *testing.T) {
	operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
	defer cancel()

	producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
		return 0, errFixtureStage, false
	})
	finalizer := pipeline.NewFinalizeStage(func(context.Context, int) error {
		test.Error(unexpectedOutputMessage)
		return nil
	})
	flow := pipeline.NewPipeline()
	flow.AppendProducer(producer)
	flow.AppendFinalizer(finalizer)

	if err := flow.Run(operationContext); !errors.Is(err, errFixtureStage) {
		test.Fatalf(unexpectedErrorFormat, err)
	}
}

func TestPipelineCancelsAndJoinsWorkers(test *testing.T) {
	operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
	defer cancel()

	nextItem := 0

	var active atomic.Int32

	started := make(chan struct{})
	releaseFailure := make(chan struct{})
	producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
		nextItem++
		return nextItem, nil, nextItem > cancellationItemCount
	})
	processor := pipeline.NewProcessStage(func(operationContext context.Context, item int) (int, error) {
		active.Add(1)
		defer active.Add(-1)

		if item == 1 {
			close(started)

			select {
			case <-releaseFailure:
				return 0, errFixtureStage
			case <-operationContext.Done():
				return 0, operationContext.Err()
			}
		}

		select {
		case <-started:
			close(releaseFailure)
		case <-operationContext.Done():
			return 0, operationContext.Err()
		}

		<-operationContext.Done()

		return 0, operationContext.Err()
	})
	finalizer := pipeline.NewFinalizeStage(func(context.Context, int) error {
		test.Error(unexpectedOutputMessage)
		return nil
	})
	flow := pipeline.NewPipeline()
	flow.AppendProducer(producer)
	flow.AppendMedium(processor)
	flow.AppendFinalizer(finalizer)

	if err := flow.Run(operationContext); !errors.Is(err, errFixtureStage) {
		test.Fatalf(unexpectedErrorFormat, err)
	}

	if active.Load() != 0 {
		test.Fatal(workersRunningMessage)
	}
}

func TestPipelinePropagatesFinalizerFailure(test *testing.T) {
	operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
	defer cancel()

	producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
		return 1, nil, false
	})
	finalizer := pipeline.NewFinalizeStage(func(context.Context, int) error {
		return errFixtureStage
	})
	flow := pipeline.NewPipeline()
	flow.AppendProducer(producer)
	flow.AppendFinalizer(finalizer)

	if err := flow.Run(operationContext); !errors.Is(err, errFixtureStage) {
		test.Fatalf(unexpectedErrorFormat, err)
	}
}

func TestPipelineHonorsCallerCancellation(test *testing.T) {
	operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
	defer cancel()

	var active atomic.Int32

	producer := pipeline.NewProduceStage(func(callbackContext context.Context) (int, error, bool) {
		active.Add(1)
		defer active.Add(-1)

		cancel()
		<-callbackContext.Done()

		return 0, callbackContext.Err(), false
	})
	finalizer := pipeline.NewFinalizeStage(func(context.Context, int) error {
		test.Error(unexpectedOutputMessage)
		return nil
	})
	flow := pipeline.NewPipeline()
	flow.AppendProducer(producer)
	flow.AppendFinalizer(finalizer)

	if err := flow.Run(operationContext); !errors.Is(err, context.Canceled) {
		test.Fatalf(unexpectedErrorFormat, err)
	}

	if active.Load() != 0 {
		test.Fatal(workersRunningMessage)
	}
}

func TestPipelineDoesNotStartCanceledCallbacks(test *testing.T) {
	operationContext, cancel := context.WithCancel(context.Background())
	cancel()

	producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
		test.Error(unexpectedOutputMessage)
		return 0, nil, true
	})
	finalizer := pipeline.NewFinalizeStage(func(context.Context, int) error {
		test.Error(unexpectedOutputMessage)
		return nil
	})
	flow := pipeline.NewPipeline()
	flow.AppendProducer(producer)
	flow.AppendFinalizer(finalizer)

	if err := flow.Run(operationContext); !errors.Is(err, context.Canceled) {
		test.Fatalf(unexpectedErrorFormat, err)
	}
}

func TestPipelineRejectsMissingStages(test *testing.T) {
	const acceptedMessage = "accepted pipeline without a producer or finalizer"

	producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
		return 0, nil, true
	})
	finalizer := pipeline.NewFinalizeStage(func(context.Context, int) error {
		return nil
	})

	for _, includeProducer := range []bool{false, true} {
		flow := pipeline.NewPipeline()
		if includeProducer {
			flow.AppendProducer(producer)
		} else {
			flow.AppendFinalizer(finalizer)
		}

		if err := flow.Run(context.Background()); err == nil {
			test.Fatal(acceptedMessage)
		}
	}
}

func TestPipelineRejectsMismatchedStages(test *testing.T) {
	const acceptedMessage = "accepted incompatible stage types"

	for _, mismatchProcessor := range []bool{false, true} {
		operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
		producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
			return 1, nil, false
		})
		processor := pipeline.NewProcessStage(func(context.Context, string) (string, error) {
			test.Error(unexpectedOutputMessage)
			return "", nil
		})
		finalizer := pipeline.NewFinalizeStage(func(context.Context, string) error {
			test.Error(unexpectedOutputMessage)
			return nil
		})
		flow := pipeline.NewPipeline()
		flow.AppendProducer(producer)

		if mismatchProcessor {
			flow.AppendMedium(processor)
		}

		flow.AppendFinalizer(finalizer)

		err := flow.Run(operationContext)

		cancel()

		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			test.Fatalf(acceptedMessage+": %v", err)
		}
	}
}

func TestPipelineMultipleProducersAndFinalizers(test *testing.T) {
	const (
		producerCount         = 2
		finalizerCount        = 3
		unexpectedCountFormat = "item %d delivered %d times"
	)

	operationContext, cancel := context.WithTimeout(context.Background(), pipelineTestDeadline)
	defer cancel()

	delivered := make([]atomic.Int32, producerCount*stageItemCount)
	flow := pipeline.NewPipeline()

	for producerIndex := range producerCount {
		nextItem := 0
		producer := pipeline.NewProduceStage(func(context.Context) (int, error, bool) {
			if nextItem == stageItemCount {
				return 0, nil, true
			}

			item := producerIndex*stageItemCount + nextItem
			nextItem++

			return item, nil, false
		})
		flow.AppendProducer(producer)
	}

	for range finalizerCount {
		finalizer := pipeline.NewFinalizeStage(func(_ context.Context, item int) error {
			delivered[item].Add(1)
			return nil
		})
		flow.AppendFinalizer(finalizer)
	}

	if err := flow.Run(operationContext); err != nil {
		test.Fatal(err)
	}

	for item := range delivered {
		if count := delivered[item].Load(); count != 1 {
			test.Errorf(unexpectedCountFormat, item, count)
		}
	}
}
