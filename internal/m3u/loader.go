package m3u

type taskType[TResult any] = func() TResult

type parLoader[TResult any] interface {
	start(workers int, takss chan taskType[TResult]) chan TResult
	stop()
}

type parLoaderImpl[TResult any] struct {
	workers int
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func newParLoader[TResult any]() parLoader[TResult] {
	return &parLoaderImpl[TResult]{}
}

func (wp *parLoaderImpl[TResult]) start(workers int, tasks chan taskType[TResult]) chan TResult {
	wp.workers = workers
	wp.stopCh = make(chan struct{})
	wp.doneCh = make(chan struct{})

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
		for range wp.workers {
			<-allDone
		}
		close(resultCh)
		close(wp.doneCh)
	}()

	return resultCh
}

func (wp *parLoaderImpl[TResult]) stop() {
	close(wp.stopCh)
	<-wp.doneCh
}
