package util

import "sync"

type PipelineStage interface {
	Run(inputCh <-chan any, stopCh chan struct{}, asProducer bool) <-chan any
}

func NewPipelineStage[TInput, TOutput any](queueSize int, workerCount int, processFunc func(TInput) (TOutput, bool)) PipelineStage {
	return &pipelineStageImpl[TInput, TOutput]{
		processFunc: processFunc,
		workerPool:  NewWorkerPool[TOutput](queueSize),
		queueSize:   queueSize,
	}
}

type pipelineStageImpl[TInput, TOutput any] struct {
	workerPool  WorkerPool[TOutput]
	queueSize   int
	workers     int
	processFunc func(TInput) (TOutput, bool)
}

func (s *pipelineStageImpl[TInput, TOutput]) Run(inputCh <-chan any, stopCh chan struct{}, asProducer bool) <-chan any {
	if asProducer {
		outputCh := make(chan any, s.queueSize)
		defer close(outputCh)
		go func() {
			for {
				var dummyInput TInput
				out, done := s.processFunc(dummyInput)
				if done {
					return
				}

				select {
				case <-stopCh:
					return
				case outputCh <- out:
				}
			}
		}()

		return outputCh
	}

	defer s.workerPool.DrainAndStop()
	outputCh := s.workerPool.Start(s.workers)

	go func() {
		for {
			select {
			case <-stopCh:
				return
			case inputI, ok := <-inputCh:
				if !ok {
					return
				}
				input := inputI.(TInput)
				task := func() TOutput {
					out, _ := s.processFunc(input)
					return out
				}
				s.workerPool.Submit(task)
			}
		}
	}()

	return outputCh
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
	if len(p.stages) == 1 {
		panic("cannot do pipeline with only producer without consumers")
	}

	wg := sync.WaitGroup{}

	var inputCh <-chan any
	stopCh := make(chan struct{})
	for i, stage := range p.stages {
		inputCh = stage.Run(inputCh, stopCh, i == 0 /*producer mode*/)
	}
	wg.Wait()
	close(stopCh)
}
