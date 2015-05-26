package pool

import (
	"runtime"
	"sync"
)

type Pool struct {
	wg         *sync.WaitGroup
	completion chan bool
	m          sync.Mutex
}

const (
	MaxConcurrencyLimit = -1
)

func New(concurrencyLimit int) *Pool {
	if concurrencyLimit == MaxConcurrencyLimit {
		concurrencyLimit = runtime.NumCPU()
	}

	wg := sync.WaitGroup{}
	completionChan := make(chan bool, concurrencyLimit)

	for i := 0; i < concurrencyLimit; i++ {
		completionChan <- true
	}

	return &Pool{&wg, completionChan, sync.Mutex{}}
}

func (pool *Pool) Spawn(job func()) {
	<-pool.completion
	pool.wg.Add(1)

	go func() {
		defer func() {
			pool.completion <- true
			pool.wg.Done()
		}()

		job()
	}()
}

func (pool *Pool) Lock() {
	pool.m.Lock()
}

func (pool *Pool) Unlock() {
	pool.m.Unlock()
}

func (pool *Pool) Wait() {
	pool.wg.Wait()
}
