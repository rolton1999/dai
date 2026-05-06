package util

import "sync"

type PipelineStage interface {
	Run(inputCh <-chan any, stopCh chan struct{}) <-chan any
}

func NewPipelineStage[TInput, TOutput any](queueSize int, workerCount int, processFunc func(TInput) (TOutput, bool)) PipelineStage {
	return &pipelineStageImpl[TInput, TOutput]{
		processFunc: processFunc,
		workerPool:  NewWorkerPool[TOutput](queueSize),
		outputCh:    make(chan any),
	}
}

type pipelineStageImpl[TInput, TOutput any] struct {
	workerPool  WorkerPool[TOutput]
	workers     int
	processFunc func(TInput) (TOutput, bool)
	outputCh    chan any
}

func (s *pipelineStageImpl[TInput, TOutput]) Run(inputCh <-chan any, stopCh chan struct{}) <-chan any {
	defer s.workerPool.DrainAndStop()
	outputCh := s.workerPool.Start(s.workers)

	// go func() {
	// 	for {
	// 		select {
	// 		case <-stopCh:
	// 			return
	// 		case inputI, ok := <-inputCh:
	// 			if !ok {
	// 				return
	// 			}
	// 			input := inputI.(TInput)
	// 			task := func() TOutput {
	// 				return s.processFunc(input)
	// 			}
	// 			task := input.(TaskType[TOutput])
	// 			s.workerPool.Submit(task)
	// 		}
	// 	}
	// }()

	return outputCh
}

func (s *pipelineStageImpl[TInput, TOutput]) GetOutputCh() <-chan any {
	return s.outputCh
}

type Pipeline interface {
	AppendStage(stage PipelineStage)
	Run()
}

func NewPipeline() Pipeline {
	return &pipelineImpl{}
}

type pipelineImpl struct {
	stages []PipelineStage
}

func (p *pipelineImpl) AppendStage(stage PipelineStage) {
	p.stages = append(p.stages, stage)
}

func (p *pipelineImpl) Run() {
	if len(p.stages) == 0 {
		return
	}

	wg := sync.WaitGroup{}

	var inputCh <-chan any
	stopCh := make(chan struct{})
	for _, stage := range p.stages {
		inputCh = stage.Run(inputCh, stopCh)
	}
	wg.Wait()
	close(stopCh)
}
