package pipeline

import (
	"context"
	"errors"
	"sync"
)

const (
	pipelineRequiresProducerAndFinalizerMessage = "pipeline requires producer and finalizer"
	pipelineMissingCallbackMessage              = "pipeline stage callback is required"
)

// Callbacks must honor their context. Run cancels on the first failure and joins
// every worker before returning, so callers can safely release owned resources.
type Pipeline[TItem any] interface {
	AppendProducer(ProduceStage[TItem])
	AppendMedium(ProcessStage[TItem])
	AppendFinalizer(FinalizeStage[TItem])
	Run(context.Context) error
}

func NewPipeline[TItem any]() Pipeline[TItem] {
	return &pipelineImpl[TItem]{}
}

type pipelineImpl[TItem any] struct {
	producers  []ProduceStage[TItem]
	mediums    []ProcessStage[TItem]
	finalizers []FinalizeStage[TItem]
}

func (pipeline *pipelineImpl[TItem]) AppendProducer(stage ProduceStage[TItem]) {
	pipeline.producers = append(pipeline.producers, stage)
}

func (pipeline *pipelineImpl[TItem]) AppendMedium(stage ProcessStage[TItem]) {
	pipeline.mediums = append(pipeline.mediums, stage)
}

func (pipeline *pipelineImpl[TItem]) AppendFinalizer(stage FinalizeStage[TItem]) {
	pipeline.finalizers = append(pipeline.finalizers, stage)
}

func (pipeline *pipelineImpl[TItem]) Run(operationContext context.Context) error {
	if len(pipeline.producers) == 0 || len(pipeline.finalizers) == 0 {
		return errors.New(pipelineRequiresProducerAndFinalizerMessage)
	}

	for _, producer := range pipeline.producers {
		if producer == nil {
			return errors.New(pipelineMissingCallbackMessage)
		}
	}

	for _, processor := range pipeline.mediums {
		if processor == nil {
			return errors.New(pipelineMissingCallbackMessage)
		}
	}

	for _, finalizer := range pipeline.finalizers {
		if finalizer == nil {
			return errors.New(pipelineMissingCallbackMessage)
		}
	}

	operationContext, cancel := context.WithCancelCause(operationContext)
	defer cancel(nil)

	report := func(err error) {
		if err != nil {
			cancel(err)
		}
	}

	var workers sync.WaitGroup

	channels := make([]<-chan TItem, 0, len(pipeline.producers))
	for _, producer := range pipeline.producers {
		channels = append(channels, producer.run(operationContext, report, &workers))
	}

	input := fanIn(operationContext, channels, &workers)
	for _, processor := range pipeline.mediums {
		input = processor.run(operationContext, input, report, &workers)
	}

	for _, finalizer := range pipeline.finalizers {
		finalizer.run(operationContext, input, report, &workers)
	}

	workers.Wait()

	return context.Cause(operationContext)
}

func fanIn[TItem any](operationContext context.Context, inputs []<-chan TItem, workers *sync.WaitGroup) <-chan TItem {
	output := make(chan TItem)

	var forwarders sync.WaitGroup
	for _, input := range inputs {
		forwarders.Add(1)
		workers.Go(func() {
			defer forwarders.Done()

			for {
				select {
				case <-operationContext.Done():
					return
				case value, open := <-input:
					if !open {
						return
					}

					select {
					case output <- value:
					case <-operationContext.Done():
						return
					}
				}
			}
		})
	}

	workers.Go(func() {
		forwarders.Wait()
		close(output)
	})

	return output
}
