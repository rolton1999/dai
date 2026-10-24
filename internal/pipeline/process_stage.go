package pipeline

import "sync"

type ProcessFunc[TInput, TOutput any] = func(TInput) (TOutput, error)

type ProcessStage interface {
	run(inputChannel <-chan any, stopChannel chan struct{}) <-chan any
}

type processStageImpl[TInput, TOutput any] struct {
	queueSize   int
	workerCount int

	process ProcessFunc[TInput, TOutput]
}

func NewProcessStage[TInput, TOutput any](process ProcessFunc[TInput, TOutput]) ProcessStage {
	const (
		defaultQueueSize   = 8
		defaultWorkerCount = 8
	)

	return &processStageImpl[TInput, TOutput]{
		queueSize:   defaultQueueSize,
		workerCount: defaultWorkerCount,
		process:     process,
	}
}

func (processor *processStageImpl[TInput, TOutput]) run(inputChannel <-chan any, stopChannel chan struct{}) <-chan any {
	outputChannel := make(chan any, processor.queueSize)

	workersDone := sync.WaitGroup{}
	for range processor.workerCount {
		workersDone.Go(func() {
			for {
				select {
				case <-stopChannel:
					return
				case inputValue, open := <-inputChannel:
					if !open {
						return
					}

					input := inputValue.(TInput)

					output, _ := processor.process(input)
					select {
					case <-stopChannel:
						return
					case outputChannel <- output:
					}
				}
			}
		})
	}

	go func() {
		workersDone.Wait()
		close(outputChannel)
	}()

	return outputChannel
}
