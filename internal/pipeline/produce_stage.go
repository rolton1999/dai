package pipeline

type ProduceFunc[TOutput any] = func() (TOutput, error, bool)

type ProduceStage interface {
	run(stopChannel chan struct{}) <-chan any
}

type produceStageImpl[TOutput any] struct {
	outputQueueSize int
	produce         ProduceFunc[TOutput]
}

func NewProduceStage[TOutput any](produce ProduceFunc[TOutput]) ProduceStage {
	const defaultOutputQueueSize = 8

	return &produceStageImpl[TOutput]{
		outputQueueSize: defaultOutputQueueSize,
		produce:         produce,
	}
}

func (producer *produceStageImpl[TOutput]) run(stopChannel chan struct{}) <-chan any {
	outputChannel := make(chan any, producer.outputQueueSize)
	go func() {
		defer close(outputChannel)

		for {
			output, _, done := producer.produce()
			if done {
				return
			}

			select {
			case <-stopChannel:
				return
			case outputChannel <- output:
			}
		}
	}()

	return outputChannel
}
