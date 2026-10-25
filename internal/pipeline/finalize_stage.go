package pipeline

import (
	"context"
	"sync"
)

type FinalizeFunc[TInput any] = func(context.Context, TInput) error

type FinalizeStage[TInput any] interface {
	run(
		context.Context,
		<-chan TInput,
		func(error),
		*sync.WaitGroup,
	)
}

func NewFinalizeStage[TInput any](finalize FinalizeFunc[TInput]) FinalizeStage[TInput] {
	if finalize == nil {
		return nil
	}

	return &finalizeStageImpl[TInput]{finalize: finalize}
}

type finalizeStageImpl[TInput any] struct {
	finalize FinalizeFunc[TInput]
}

func (finalizer *finalizeStageImpl[TInput]) run(
	operationContext context.Context,
	input <-chan TInput,
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

				if err := finalizer.finalize(operationContext, value); err != nil {
					report(err)
					return
				}
			}
		}
	})
}
