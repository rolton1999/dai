package util

import "sync"

type PipelineStage interface {
	Run(inputCh <-chan any, stopCh chan struct{})
	GetOutputCh() <-chan any
}

func NewPipelineStage[TInput, TOutput any](processFunc func(TInput) TOutput) PipelineStage {
	return &pipelineStageImpl[TInput, TOutput]{
		processFunc: processFunc,
		outputCh:    make(chan any),
	}
}

type pipelineStageImpl[TInput, TOutput any] struct {
	processFunc func(TInput) TOutput
	outputCh    chan any
}

func (s *pipelineStageImpl[TInput, TOutput]) Run(inputCh <-chan any, stopCh chan struct{}) {
	defer close(s.outputCh)
	for {
		select {
		case <-stopCh:
			return
		case input := <-inputCh:
			res := s.processFunc(input.(TInput))
			select {
			case <-stopCh:
				return
			case s.outputCh <- res:
			}
		}
	}
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
		nextInputCh := stage.GetOutputCh()
		wg.Go(func() {
			stage.Run(inputCh, stopCh)
		})
		inputCh = nextInputCh
	}
	wg.Wait()
	close(stopCh)
}
