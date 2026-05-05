package util

type TaskType[TResult any] = func() TResult

type WorkerPool[TResult any] interface {
	Start(workers int, tasks <-chan TaskType[TResult]) <-chan TResult
	Submit(task TaskType[TResult])
	Stop()
}

type workerPoolImpl[TResult any] struct {
	stopCh       chan struct{}
	allStoppedCh chan struct{}
}

func NewWorkerPool[TResult any]() WorkerPool[TResult] {
	return &workerPoolImpl[TResult]{}
}

func (wp *workerPoolImpl[TResult]) Submit(tasks TaskType[TResult]) {

}

func (wp *workerPoolImpl[TResult]) Start(workers int, tasks <-chan TaskType[TResult]) <-chan TResult {
	wp.stopCh = make(chan struct{})
	wp.allStoppedCh = make(chan struct{})

	allDone := make(chan struct{})
	resultCh := make(chan TResult)

	for range workers {
		go func() {
			defer func() { allDone <- struct{}{} }()

			for {
				select {
				case <-wp.stopCh:
					return
				case task, ok := <-tasks:
					if !ok {
						return
					}
					resultCh <- task()
				}
			}
		}()
	}

	go func() {
		for range workers {
			<-allDone
		}
		close(resultCh)
		close(wp.allStoppedCh)
	}()

	return resultCh
}

func (wp *workerPoolImpl[TResult]) Stop() {
	close(wp.stopCh)
	<-wp.allStoppedCh
}
