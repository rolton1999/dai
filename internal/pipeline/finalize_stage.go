package pipeline

type FinalizeFunc[TInput any] = func(TInput)

type FinalizeStage interface {
	run(inputChannel <-chan any, stopChannel chan struct{}) <-chan any
}

type finalizeStageImpl[TInput any] struct {
	finalize FinalizeFunc[TInput]
}

func NewFinalizeStage[TInput any](finalize FinalizeFunc[TInput]) FinalizeStage {
	return &finalizeStageImpl[TInput]{
		finalize: finalize,
	}
}

func (finalizer *finalizeStageImpl[TInput]) run(inputChannel <-chan any, stopChannel chan struct{}) <-chan any {
	outputChannel := make(chan any)
	go func() {
		defer close(outputChannel)

		for {
			select {
			case <-stopChannel:
				return
			case inputValue, open := <-inputChannel:
				if !open {
					return
				}

				input := inputValue.(TInput)
				finalizer.finalize(input)
			}
		}
	}()

	return outputChannel
}
