package pipeline

import (
	"context"
	"errors"
	"sync"
)

const (
	pipelineStageTypeMismatchMessage = "pipeline stage type mismatch"
)

const (
	processorQueueSize   = 8
	processorWorkerCount = 8
)

type ProcessFunc[TInput, TOutput any] = func(context.Context, TInput) (TOutput, error)

type ProcessStage interface {
	run(
		context.Context,
		<-chan any,
		func(error),
		*sync.WaitGroup,
	) <-chan any
}

func NewProcessStage[TInput, TOutput any](process ProcessFunc[TInput, TOutput]) ProcessStage {
	return &processStageImpl[TInput, TOutput]{process: process}
}

type processStageImpl[TInput, TOutput any] struct {
	process ProcessFunc[TInput, TOutput]
}

func (processor *processStageImpl[TInput, TOutput]) run(
	operationContext context.Context,
	input <-chan any,
	report func(error),
	workers *sync.WaitGroup,
) <-chan any {
	output := make(chan any, processorQueueSize)

	var processors sync.WaitGroup
	for range processorWorkerCount {
		processors.Add(1)
		workers.Go(func() {
			defer processors.Done()

			for {
				select {
				case <-operationContext.Done():
					return
				case value, open := <-input:
					if !open {
						return
					}

					if operationContext.Err() != nil {
						return
					}

					typed, valid := value.(TInput)
					if !valid {
						report(errors.New(pipelineStageTypeMismatchMessage))
						return
					}

					result, err := processor.process(operationContext, typed)
					if err != nil {
						report(err)
						return
					}

					select {
					case output <- result:
					case <-operationContext.Done():
						return
					}
				}
			}
		})
	}

	workers.Go(func() {
		processors.Wait()
		close(output)
	})

	return output
}
