package pipeline

import "sync"

type Pipeline interface {
	AppendProducer(ProduceStage)
	AppendMedium(ProcessStage)
	AppendFinalizer(FinalizeStage)

	Run()
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

func (pipeline *pipelineImpl) Run() {
	if len(pipeline.producers) == 0 {
		panic("cannot do pipeline without any producers")
	}

	if len(pipeline.finalizers) == 0 {
		panic("cannot do pipeline without any finalizers")
	}

	stopChannel := make(chan struct{})

	producerChannels := make([]<-chan any, len(pipeline.producers))
	for stageIndex := range pipeline.producers {
		producerChannels[stageIndex] = pipeline.producers[stageIndex].run(stopChannel)
	}

	inputChannel := fanIn(producerChannels...)
	for stageIndex := range pipeline.mediums {
		inputChannel = pipeline.mediums[stageIndex].run(inputChannel, stopChannel)
	}

	finalizerInputs := fanOut(inputChannel, len(pipeline.finalizers))

	finalizerCompletionChannels := make([]<-chan any, len(pipeline.finalizers))
	for stageIndex := range pipeline.finalizers {
		finalizerCompletionChannels[stageIndex] = pipeline.finalizers[stageIndex].run(finalizerInputs[stageIndex], stopChannel)
	}

	<-fanIn(finalizerCompletionChannels...)

	close(stopChannel)
}

func fanIn(inputChannels ...<-chan any) <-chan any {
	outputChannel := make(chan any)
	workersDone := sync.WaitGroup{}

	for _, inputChannel := range inputChannels {
		workersDone.Go(func() {
			for value := range inputChannel {
				outputChannel <- value
			}
		})
	}

	go func() {
		workersDone.Wait()
		close(outputChannel)
	}()

	return outputChannel
}

func fanOut(inputChannel <-chan any, finalizerCount int) []chan any {
	outputChannels := make([]chan any, finalizerCount)
	for stageIndex := range outputChannels {
		outputChannels[stageIndex] = make(chan any)
	}

	for stageIndex := range finalizerCount {
		go func() {
			defer close(outputChannels[stageIndex])

			for value := range inputChannel {
				outputChannels[stageIndex] <- value
			}
		}()
	}

	return outputChannels
}
