package pipeline

import (
	"context"
	"sync"
)

const producerQueueSize = 8

type ProduceFunc[TOutput any] = func(context.Context) (TOutput, error, bool)

type ProduceStage interface {
	run(context.Context, func(error), *sync.WaitGroup) <-chan any
}

func NewProduceStage[TOutput any](produce ProduceFunc[TOutput]) ProduceStage {
	return &produceStageImpl[TOutput]{produce: produce}
}

type produceStageImpl[TOutput any] struct {
	produce ProduceFunc[TOutput]
}

func (producer *produceStageImpl[TOutput]) run(operationContext context.Context, report func(error), workers *sync.WaitGroup) <-chan any {
	output := make(chan any, producerQueueSize)

	workers.Go(func() {
		defer close(output)

		for {
			if operationContext.Err() != nil {
				return
			}

			value, err, done := producer.produce(operationContext)
			if err != nil {
				report(err)
				return
			}

			if done {
				return
			}

			select {
			case output <- value:
			case <-operationContext.Done():
				return
			}
		}
	})

	return output
}
