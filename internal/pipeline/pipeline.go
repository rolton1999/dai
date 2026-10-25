package pipeline

import (
	"context"
	"errors"
	"sync"
)

const (
	pipelineRequiresProducerAndFinalizerMessage = "pipeline requires producer and finalizer"
)

// Callbacks must honor their context. Run cancels on the first failure and joins
// every worker before returning, so callers can safely release owned resources.
type Pipeline interface {
	AppendProducer(ProduceStage)
	AppendMedium(ProcessStage)
	AppendFinalizer(FinalizeStage)
	Run(context.Context) error
}

func NewPipeline() Pipeline {
	return &pipelineImpl{}
}

type pipelineImpl struct {
	producers  []ProduceStage
	mediums    []ProcessStage
	finalizers []FinalizeStage
}

func (pipeline *pipelineImpl) AppendProducer(stage ProduceStage) {
	pipeline.producers = append(pipeline.producers, stage)
}

func (pipeline *pipelineImpl) AppendMedium(stage ProcessStage) {
	pipeline.mediums = append(pipeline.mediums, stage)
}

func (pipeline *pipelineImpl) AppendFinalizer(stage FinalizeStage) {
	pipeline.finalizers = append(pipeline.finalizers, stage)
}

func (pipeline *pipelineImpl) Run(operationContext context.Context) error {
	if len(pipeline.producers) == 0 || len(pipeline.finalizers) == 0 {
		return errors.New(pipelineRequiresProducerAndFinalizerMessage)
	}

	operationContext, cancel := context.WithCancelCause(operationContext)
	defer cancel(nil)

	report := func(err error) {
		if err != nil {
			cancel(err)
		}
	}

	var workers sync.WaitGroup

	channels := make([]<-chan any, 0, len(pipeline.producers))
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

func fanIn(operationContext context.Context, inputs []<-chan any, workers *sync.WaitGroup) <-chan any {
	output := make(chan any)

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
