package main

import "sync"

type WorkerPool interface {
	Run()
	AddTask(task func())
	Wait()
}

type workerPool struct {
	maxWorkers  int
	queuedTaskC chan func()
	wg          sync.WaitGroup
}

// NewWorkerPool will create an instance of WorkerPool.
func NewWorkerPool(maxWorkers int) WorkerPool {
	wp := &workerPool{
		maxWorkers:  maxWorkers,
		queuedTaskC: make(chan func()),
	}

	return wp
}

func (wp *workerPool) Run() {
	wp.run()
}

func (wp *workerPool) AddTask(task func()) {
	wp.wg.Add(1)
	wp.queuedTaskC <- task
}

func (wp *workerPool) GetTotalQueuedTask() int {
	return len(wp.queuedTaskC)
}

func (wp *workerPool) run() {
	for w := 0; w < wp.maxWorkers; w++ {
		go func() {
			for task := range wp.queuedTaskC {
				task()
				wp.wg.Done()
			}
		}()
	}
}

func (wp *workerPool) Wait() {
	wp.wg.Wait()
}
