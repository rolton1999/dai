package util

type stageRole int

const (
	producer = iota
	medium
	finalizer
)

type PipelineStage interface {
	Run(inputCh <-chan any, stopCh chan struct{}, role stageRole) <-chan any
}

func NewPipelineStage[TInput, TOutput any](queueSize int, workerCount int, processFunc func(TInput) (TOutput, bool)) PipelineStage {
	return &pipelineStageImpl[TInput, TOutput]{
		processFunc: processFunc,
		workerPool:  NewWorkerPool[TOutput](queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
	}
}

type pipelineStageImpl[TInput, TOutput any] struct {
	workerPool  WorkerPool[TOutput]
	queueSize   int
	workerCount int

	processFunc func(TInput) (TOutput, bool)
}

func (s *pipelineStageImpl[TInput, TOutput]) Run(inputCh <-chan any, stopCh chan struct{}, role stageRole) <-chan any {
	if role == producer {
		outputCh := make(chan any, s.queueSize)
		go func() {
			defer close(outputCh)
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

	if role == finalizer {
		outputCh := make(chan any, s.queueSize)
		go func() {
			defer close(outputCh)
			for {
				select {
				case <-stopCh:
					return
				case inputI, ok := <-inputCh:
					if !ok {
						return
					}
					input := inputI.(TInput)
					s.processFunc(input)
				}
			}
		}()

		return outputCh
	}

	outputCh := s.workerPool.Start(s.workerCount)

	go func() {
		defer s.workerPool.DrainAndStop()
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
		panic("cannot do pipeline without any stages")
	}
	if len(p.stages) == 1 {
		panic("cannot do pipeline with only producer without consumers")
	}

	var inputCh <-chan any
	stopCh := make(chan struct{})
	for i, stage := range p.stages {
		var role stageRole = medium
		if i == 0 {
			role = producer
		} else if i == len(p.stages)-1 {
			role = finalizer
		}
		inputCh = stage.Run(inputCh, stopCh, role)
	}

	// finalizer should simply close this ch
	<-inputCh

	close(stopCh)
}
