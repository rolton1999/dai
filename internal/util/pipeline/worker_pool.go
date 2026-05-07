package pipeline

type TaskType[TResult any] = func() TResult

type WorkerPool[TResult any] interface {
	Start(workers int) <-chan any
	Submit(task TaskType[TResult])
	GetSubmitCh() chan<- TaskType[TResult]
	DrainAndStop()
	Stop()
}

type workerPoolImpl[TResult any] struct {
	tasks        chan TaskType[TResult]
	stopCh       chan struct{}
	allStoppedCh chan struct{}
}

func NewWorkerPool[TResult any](queueSize int) WorkerPool[TResult] {
	return &workerPoolImpl[TResult]{
		tasks:        make(chan TaskType[TResult], queueSize),
		stopCh:       make(chan struct{}),
		allStoppedCh: make(chan struct{}),
	}
}

func (wp *workerPoolImpl[TResult]) Submit(task TaskType[TResult]) {
	select {
	case wp.tasks <- task:
	case <-wp.stopCh:
	}
}

func (wp *workerPoolImpl[TResult]) GetSubmitCh() chan<- TaskType[TResult] {
	return wp.tasks
}

func (wp *workerPoolImpl[TResult]) Start(workers int) <-chan any {
	allDone := make(chan struct{})
	resultCh := make(chan any)

	for range workers {
		go func() {
			defer func() {
				allDone <- struct{}{}
			}()

			for {
				select {
				case <-wp.stopCh:
					return
				case task, ok := <-wp.tasks:
					if !ok {
						return
					}
					if task == nil {
						continue
					}
					out := task()
					select {
					case resultCh <- out:
					case <-wp.stopCh:
					}
				}
			}
		}()
	}

	go func() {
		for range workers {
			<-allDone
		}
		close(allDone)
		close(resultCh)
		close(wp.allStoppedCh)
	}()

	return resultCh
}

func (wp *workerPoolImpl[TResult]) DrainAndStop() {
	close(wp.tasks)
	// all workers done
	<-wp.allStoppedCh
}

func (wp *workerPoolImpl[TResult]) Stop() {
	close(wp.stopCh)
	<-wp.allStoppedCh
}
