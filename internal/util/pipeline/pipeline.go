package pipeline

import (
	"sync"
)

type stageRole int

const (
	producer = iota
	medium
	finalizer
)

type produceFn[TOutput any] = func() (TOutput, error, bool)
type processFn[TInput, TOutput any] = func(TInput) (TOutput, error)
type finalizeFn[TInput any] = func(TInput) error

type Stage interface {
	run(inputCh <-chan any, stopCh chan struct{}, role stageRole) <-chan any
}

func NewProducerStage[TOutput any](queueSize int, fn produceFn[TOutput]) Stage {
	return &stageImpl[any, TOutput]{
		fn: func(any) (TOutput, error, bool) {
			out, err, done := fn()
			return out, err, done
		},
		queueSize: queueSize,
	}
}

func NewMediumStage[TInput, TOutput any](queueSize int, workerCount int, fn processFn[TInput, TOutput]) Stage {
	return &stageImpl[TInput, TOutput]{
		fn: func(input TInput) (TOutput, error, bool) {
			out, err := fn(input)
			return out, err, false
		},
		workerPool:  NewWorkerPool[TOutput](queueSize),
		queueSize:   queueSize,
		workerCount: workerCount,
	}
}

func NewFinalizerStage[TInput any](queueSize int, fn finalizeFn[TInput]) Stage {
	return &stageImpl[TInput, any]{
		fn: func(input TInput) (any, error, bool) {
			err := fn(input)
			return struct{}{}, err, true
		},
		queueSize: queueSize,
	}
}

type stageImpl[TInput, TOutput any] struct {
	workerPool  WorkerPool[TOutput]
	queueSize   int
	workerCount int

	fn func(TInput) (TOutput, error, bool)
}

// how to stop producers?
func (s *stageImpl[TInput, TOutput]) runAsProcucer(stopCh chan struct{}) <-chan any {
	outputCh := make(chan any, s.queueSize)
	go func() {
		defer close(outputCh)
		var dummyInput TInput

		for {
			out, _, done := s.fn(dummyInput)
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
					out, _, _ := s.fn(input)
					return out
				}
				select {
				case <-stopCh:
					return
				case submitCh <- task:
				}
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
				s.fn(input)
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
	p.producers = append(p.producers, stage)
	p.stages = append(p.stages, stage)
}

func (p *pipelineImpl) AppendMedium(stage Stage) {
	p.mediums = append(p.mediums, stage)
	p.stages = append(p.stages, stage)
}

func (p *pipelineImpl) AppendFinalizer(stage Stage) {
	p.finalizers = append(p.finalizers, stage)
	p.stages = append(p.stages, stage)
}

func (p *pipelineImpl) Run() {
	if len(p.producers) == 0 {
		panic("cannot do pipeline without any producers")
	}
	if len(p.finalizers) == 0 {
		panic("cannot do pipeline without any finalizers")
	}

	var inputCh <-chan any
	stopCh := make(chan struct{})
	producersChs := make([]<-chan any, len(p.producers))
	for i := range p.producers {
		producersChs[i] = p.producers[i].run(inputCh, stopCh, producer)
	}

	inputCh = fanIn(producersChs...)
	for i := range p.mediums {
		inputCh = p.mediums[i].run(inputCh, stopCh, medium)
	}

	consumersInputsChs := fanOut(inputCh, len(p.finalizers))
	finalizedChs := make([]<-chan any, len(p.finalizers))
	for i := range p.finalizers {
		finalizedChs[i] = p.finalizers[i].run(consumersInputsChs[i], stopCh, finalizer)
	}
	// for i, stage := range p.stages {
	// 	var role stageRole = medium
	// 	if i == 0 {
	// 		role = producer
	// 	} else if i == len(p.stages)-1 {
	// 		role = finalizer
	// 	}
	// 	inputCh = stage.run(inputCh, stopCh, role)
	// }

	// finalizer should simply close this ch
	<-fanIn(finalizedChs...)

	close(stopCh)
}

func fanIn(chans ...<-chan any) <-chan any {
	outCh := make(chan any)
	wg := sync.WaitGroup{}

	for _, ch := range chans {
		wg.Go(func() {
			for v := range ch {
				outCh <- v
			}
		})
	}

	go func() {
		wg.Wait()
		close(outCh)
	}()

	return outCh
}

func fanOut(inputCh <-chan any, howMany int) []chan any {
	outChs := make([]chan any, howMany)
	for i := range outChs {
		outChs[i] = make(chan any)
	}

	wg := sync.WaitGroup{}
	for i := range howMany {
		wg.Go(func() {
			defer close(outChs[i])
			for v := range inputCh {
				outChs[i] <- v
			}
		})
	}
	go func() {
		wg.Wait()
	}()
	return outChs
}
