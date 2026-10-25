package pipeline

import (
	"context"
	"sync"
)

const (
	processorQueueSize   = 8
	processorWorkerCount = 8
)

type ProcessFunc[TItem any] = func(context.Context, TItem) (TItem, error)

type ProcessStage[TItem any] interface {
	run(
		context.Context,
		<-chan TItem,
		func(error),
		*sync.WaitGroup,
	) <-chan TItem
}

func NewProcessStage[TItem any](process ProcessFunc[TItem]) ProcessStage[TItem] {
	if process == nil {
		return nil
	}

	return &processStageImpl[TItem]{process: process}
}

type processStageImpl[TItem any] struct {
	process ProcessFunc[TItem]
}

func (processor *processStageImpl[TItem]) run(
	operationContext context.Context,
	input <-chan TItem,
	report func(error),
	workers *sync.WaitGroup,
) <-chan TItem {
	output := make(chan TItem, processorQueueSize)

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

					result, err := processor.process(operationContext, value)
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
