package pipeline

import (
	"sync"
)

type ProduceFn[TOutput any] = func() (TOutput, error, bool)
type ProcessFn[TInput, TOutput any] = func(TInput) (TOutput, error)
type FinalizeFn[TInput any] = func(TInput) error

type ProduceStage interface {
	run(stopCh chan struct{}) <-chan any
}

type ProcessStage interface {
	run(inputCh <-chan any, stopCh chan struct{}) <-chan any
}

type FinalizeStage interface {
	run(inputCh <-chan any, stopCh chan struct{}) <-chan any
}

type produceStageImpl[TOutput any] struct {
	outputQueueSize int
	fn              ProduceFn[TOutput]
}

func NewProduceStage[TOutput any](fn ProduceFn[TOutput]) ProduceStage {
	const outputQueueSize = 8
	return &produceStageImpl[TOutput]{
		outputQueueSize: outputQueueSize,
		fn:              fn,
	}
}

func (s *produceStageImpl[TOutput]) run(stopCh chan struct{}) <-chan any {
	outputCh := make(chan any, s.outputQueueSize)
	go func() {
		defer close(outputCh)
		for {
			out, _, done := s.fn()
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

type processStageImpl[TInput, TOutput any] struct {
	workerPool  WorkerPool[TOutput]
	queueSize   int
	workerCount int

	fn ProcessFn[TInput, TOutput]
}

func NewProcessStage[TInput, TOutput any](fn ProcessFn[TInput, TOutput]) ProcessStage {
	const (
		queueSize = 8
		workers   = 8
	)
	return &processStageImpl[TInput, TOutput]{
		workerPool:  NewWorkerPool[TOutput](queueSize),
		queueSize:   queueSize,
		workerCount: workers,
		fn:          fn,
	}
}

func (s *processStageImpl[TInput, TOutput]) run(inputCh <-chan any, stopCh chan struct{}) <-chan any {
	outputCh := make(chan any, s.queueSize)
	wg := sync.WaitGroup{}
	for range s.workerCount {
		wg.Go(func() {
			for {
				select {
				case <-stopCh:
					return
				case inputI, ok := <-inputCh:
					if !ok {
						return
					}
					input := inputI.(TInput)
					out, _ := s.fn(input)
					select {
					case <-stopCh:
						return
					case outputCh <- out:
					}
				}
			}
		})
	}

	go func() {
		wg.Wait()
		close(outputCh)
	}()

	return outputCh
}

type finalizeStageImpl[TInput any] struct {
	fn FinalizeFn[TInput]
}

func NewFinalizeStage[TInput any](fn FinalizeFn[TInput]) FinalizeStage {
	return &finalizeStageImpl[TInput]{
		fn: fn,
	}
}

func (s *finalizeStageImpl[TInput]) run(inputCh <-chan any, stopCh chan struct{}) <-chan any {
	outputCh := make(chan any)
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

func (p *pipelineImpl) AppendProducer(stage ProduceStage) {
	p.producers = append(p.producers, stage)
}

func (p *pipelineImpl) AppendMedium(stage ProcessStage) {
	p.mediums = append(p.mediums, stage)
}

func (p *pipelineImpl) AppendFinalizer(stage FinalizeStage) {
	p.finalizers = append(p.finalizers, stage)
}

func (p *pipelineImpl) Run() {
	if len(p.producers) == 0 {
		panic("cannot do pipeline without any producers")
	}
	if len(p.finalizers) == 0 {
		panic("cannot do pipeline without any finalizers")
	}

	stopCh := make(chan struct{})
	producersChs := make([]<-chan any, len(p.producers))
	for i := range p.producers {
		producersChs[i] = p.producers[i].run(stopCh)
	}

	inputCh := fanIn(producersChs...)
	for i := range p.mediums {
		inputCh = p.mediums[i].run(inputCh, stopCh)
	}

	consumersInputsChs := fanOut(inputCh, len(p.finalizers))
	finalizedChs := make([]<-chan any, len(p.finalizers))
	for i := range p.finalizers {
		finalizedChs[i] = p.finalizers[i].run(consumersInputsChs[i], stopCh)
	}
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
