package pipeline

type stageRole int

const (
	producer = iota
	medium
	finalizer
)

type Stage interface {
	run(inputCh <-chan any, stopCh chan struct{}, role stageRole) <-chan any
}

func NewStage[TInput, TOutput any](queueSize int, workerCount int, processFunc func(TInput) TOutput) Stage {
	return &stageImpl[TInput, TOutput]{
		processFunc: func(input TInput) (TOutput, bool) { return processFunc(input), true },
		workerPool:  NewWorkerPool[TOutput](queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
	}
}

func NewStageWithoutInput[TOutput any](queueSize int, workerCount int, processFunc func() (TOutput, bool)) Stage {
	return &stageImpl[any, TOutput]{
		processFunc: func(any) (TOutput, bool) { return processFunc() },
		workerPool:  NewWorkerPool[TOutput](queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
	}
}

func NewStageWithoutOutput[TInput any](queueSize int, workerCount int, processFunc func(TInput)) Stage {
	return &stageImpl[TInput, any]{
		processFunc: func(input TInput) (any, bool) {
			processFunc(input)
			return struct{}{}, true
		},
		workerPool:  NewWorkerPool[any](queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
	}
}

type stageImpl[TInput, TOutput any] struct {
	workerPool  WorkerPool[TOutput]
	queueSize   int
	workerCount int

	processFunc func(TInput) (TOutput, bool)
}

// how to stop producers?
func (s *stageImpl[TInput, TOutput]) runAsProcucer(stopCh chan struct{}) <-chan any {
	outputCh := make(chan any, s.queueSize)
	// outputCh := s.workerPool.Start(s.workerCount)
	go func() {
		// defer s.workerPool.DrainAndStop()
		// submitCh := s.workerPool.GetSubmitCh()
		defer close(outputCh)
		var dummyInput TInput

		for {
			out, done := s.processFunc(dummyInput)
			if done {
				return
			}
			select {
			case <-stopCh:
				return
			case outputCh <- out:
			}
			// task := func() TOutput {
			// 	out, done := s.processFunc(dummyInput)
			// 	if done {
			// 		produceDone.Store(true)
			// 	}
			// 	return out
			// }

			// select {
			// case <-stopCh:
			// 	return
			// case submitCh <- task:
			// }
		}
	}()

	return outputCh
}

func (s *stageImpl[TInput, TOutput]) runAsMedium(inputCh <-chan any, stopCh chan struct{}) <-chan any {
	outputCh := s.workerPool.Start(s.workerCount)

	go func() {
		defer s.workerPool.DrainAndStop()
		submitCh := s.workerPool.GetSubmitCh()
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
				select {
				case <-stopCh:
					return
				case submitCh <- task:
				}
				// s.workerPool.Submit(task)
			}
		}
	}()

	return outputCh
}
func (s *stageImpl[TInput, TOutput]) runAsFinalizer(inputCh <-chan any, stopCh chan struct{}) <-chan any {
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
func (s *stageImpl[TInput, TOutput]) run(inputCh <-chan any, stopCh chan struct{}, role stageRole) <-chan any {
	switch role {
	case producer:
		return s.runAsProcucer(stopCh)
	case medium:
		return s.runAsMedium(inputCh, stopCh)
	case finalizer:
		return s.runAsFinalizer(inputCh, stopCh)
	}
	panic("unknown stage")
}

type Pipeline interface {
	AppendProducer(stage Stage)
	AppendMedium(stage Stage)
	AppendFinalizer(stage Stage)

	Run()
}

func NewPipeline() Pipeline {
	return &pipelineImpl{}
}

type pipelineImpl struct {
	producers  []Stage
	mediums    []Stage
	finalizers []Stage

	stages []Stage
}

func (p *pipelineImpl) AppendProducer(stage Stage) {
	p.stages = append(p.stages, stage)
}

func (p *pipelineImpl) AppendMedium(stage Stage) {
	p.stages = append(p.stages, stage)
}

func (p *pipelineImpl) AppendFinalizer(stage Stage) {
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
		inputCh = stage.run(inputCh, stopCh, role)
	}

	// finalizer should simply close this ch
	<-inputCh

	close(stopCh)
}
