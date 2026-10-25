package pipeline

import (
	"context"
	"errors"
	"sync"
)

const (
	pipelineFinalizerTypeMismatchMessage = "pipeline finalizer type mismatch"
)

type FinalizeFunc[TInput any] = func(context.Context, TInput) error

type FinalizeStage interface {
	run(
		context.Context,
		<-chan any,
		func(error),
		*sync.WaitGroup,
	)
}

func NewFinalizeStage[TInput any](finalize FinalizeFunc[TInput]) FinalizeStage {
	return &finalizeStageImpl[TInput]{finalize: finalize}
}

type finalizeStageImpl[TInput any] struct {
	finalize FinalizeFunc[TInput]
}

func (finalizer *finalizeStageImpl[TInput]) run(
	operationContext context.Context,
	input <-chan any,
	report func(error),
	workers *sync.WaitGroup,
) {
	workers.Go(func() {
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
					report(errors.New(pipelineFinalizerTypeMismatchMessage))
					return
				}

				if err := finalizer.finalize(operationContext, typed); err != nil {
					report(err)
					return
				}
			}
		}
	})
}
