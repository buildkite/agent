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
		// Completely arbitrary. Most of the time we could probably have unbounded concurrency, but the situations where we use
		// this pool is basically just S3 uploading and downloading, so this number is kind of a proxy for "What won't rate limit us"
		// TODO: Make artifact uploads and downloads gracefully handle rate limiting, remove this pool entirely, and use unbounded concurrency via a WaitGroup
		concurrencyLimit = runtime.NumCPU() * 10
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
