package retry

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

var defaultRandom = rand.New(rand.NewSource(time.Now().UnixNano()))

const jitterInterval = 1000 * time.Millisecond

type Retrier struct {
	maxAttempts  int
	attemptCount int
	jitter       bool
	forever      bool
	rand         *rand.Rand

	breakNext     bool
	lastAttemptAt time.Time
	sleepFunc     func(time.Duration)

	intervalCalculator strategy
	strategyType       strategyType
}

type strategy func(*Retrier) time.Duration
type strategyType string

const (
	constantStrategy    strategyType = "constant"
	exponentialStrategy strategyType = "exponential"
)

// Constant returns a strategy that always returns the same value, the interval passed in as an arg to the function
// Semantically, when this is used with a retry.retrier, it means that the retrier will always wait the given
// duration before retrying
func Constant(interval time.Duration) (strategy, strategyType) {
	if interval < 0 {
		panic("constant retry strategies must have a positive interval")
	}

	return func(r *Retrier) time.Duration {
		return interval + r.calculateJitter()
	}, constantStrategy
}

// Exponential returns a strategy that increases expontially based on the number of attempts the retrier has made
// It uses the calculation: adjustment + (base ** attempts) + jitter
func Exponential(base, adjustment time.Duration) (strategy, strategyType) {
	if base < 1*time.Second {
		panic("exponential retry strategies must have a base of at least 1 second")
	}

	return func(r *Retrier) time.Duration {
		baseSeconds := int(base / time.Second)
		exponentSeconds := math.Pow(float64(baseSeconds), float64(r.attemptCount))
		exponent := time.Duration(exponentSeconds) * time.Second

		return adjustment + exponent + r.calculateJitter()
	}, exponentialStrategy
}

type retrierOpt func(*Retrier)

// WithMaxAttempts sets the maximum number of retries that a retrier will attempt
func WithMaxAttempts(maxAttempts int) retrierOpt {
	return func(r *Retrier) {
		r.maxAttempts = maxAttempts
	}
}

func WithRand(rand *rand.Rand) retrierOpt {
	return func(r *Retrier) {
		r.rand = rand
	}
}

// WithStrategy sets the retry strategy that the retrier will use to determine how long to wait between retries
func WithStrategy(strategy strategy, strategyType strategyType) retrierOpt {
	return func(r *Retrier) {
		r.strategyType = strategyType
		r.intervalCalculator = strategy
	}
}

// WithJitter enables jitter on the retrier, which will cause all of the retries to wait a random amount of time < 1 second
// The idea here is to avoid thundering herds - retries that are in parallel will happen at slightly different times when
// jitter is enabled, whereas if jitter is disabled, all the retries might happen at the same time, causing further load
// on the system that we're tryung to do something with
func WithJitter() retrierOpt {
	return func(r *Retrier) {
		r.jitter = true
	}
}

// TryForever causes the retrier to to never give up retrying, until either the operation succeeds, or the operation
// calls retrier.Break()
func TryForever() retrierOpt {
	return func(r *Retrier) {
		r.forever = true
	}
}

// WithSleepFunc sets the function that the retrier uses to sleep between successive attempts
// Only really useful for testing
func WithSleepFunc(f func(time.Duration)) retrierOpt {
	return func(r *Retrier) {
		r.sleepFunc = f
	}
}

// NewRetrier creates a new instance of the Retrier struct. Pass in retrierOpt functions to customise the behaviour of
// the retrier
func NewRetrier(opts ...retrierOpt) *Retrier {
	r := &Retrier{
		sleepFunc: time.Sleep,
		rand:      defaultRandom,
	}

	for _, o := range opts {
		o(r)
	}

	// We use panics here rather than returning an error because all of these are logical issues caused by the programmer,
	// they should never occur in normal running, and can't be logically recovered from
	if r.maxAttempts == 0 && !r.forever {
		panic("retriers must either run forever, or have a maximum attempt count")
	}

	if r.maxAttempts < 0 {
		panic("retriers must have a positive max attempt count")
	}

	oldJitter := r.jitter
	r.jitter = false // Temporarily turn off jitter while we check if the interval is 0
	if r.forever && r.strategyType == constantStrategy && r.NextInterval() == 0 {
		panic("retriers using the constant strategy that run forever must have an interval")
	}
	r.jitter = oldJitter // and now set it back to what it was previously

	return r
}

func (r *Retrier) calculateJitter() time.Duration {
	if r.jitter {
		return time.Duration(r.rand.Float32()) * jitterInterval
	}

	return 0
}

// MarkAttempt increments the attempt count for the retrier. This affects ShouldGiveUp, and also affects the retry interval
// for Exponential retry strategy
func (r *Retrier) MarkAttempt() {
	r.attemptCount += 1
	r.lastAttemptAt = time.Now()
}

// Break causes the Retrier to stop retrying after it completes the next retry cycle
func (r *Retrier) Break() {
	r.breakNext = true
}

// ShouldGiveUp returns whether the retrier should stop trying do do the thing it's been asked to do
// It returns true if the retry count is greater than r.maxAttempts, or if r.Break() has been called
// It returns false if the retrier is supposed to try forever
func (r *Retrier) ShouldGiveUp() bool {
	if r.breakNext {
		return true
	}

	if r.forever {
		return false
	}

	return r.attemptCount >= r.maxAttempts
}

// NextInterval returns the next interval that the retrier will use. Behind the scenes, it calls the function generated
// by either retrier's strategy
func (r *Retrier) NextInterval() time.Duration {
	return r.intervalCalculator(r)
}

func (r *Retrier) String() string {
	str := fmt.Sprintf("Attempt %d/", r.attemptCount)

	if r.forever {
		str = str + "∞"
	} else {
		str = str + fmt.Sprintf("%d", r.maxAttempts)
	}

	nextInterval := r.NextInterval()
	if nextInterval > 0 {
		str = str + fmt.Sprintf(" Retrying in %s", nextInterval-time.Since(r.lastAttemptAt))
	} else {
		str = str + " Retrying immediately"
	}

	return str
}

func (r *Retrier) AttemptCount() int {
	return r.attemptCount
}

// Do is the core loop of a Retrier. It defines the operation that the Retrier will attempt to perform, retrying it if necessary
// Calling retrier.Do(someFunc) will cause the Retrier to attempt to call the function, and if it returns an error,
// retry it using the settings provided to it.
func (r *Retrier) Do(callback func(*Retrier) error) error {
	var err error
	for {
		// Perform the action the user has requested we retry
		err = callback(r)
		if err == nil {
			return nil
		}

		// Calculate the next interval before we increment the attempt count
		// In the exponential case, if we didn't do this, we'd skip the first interval
		// ie, we would wait 2^1, 2^2, 2^3, ..., 2^n+1 seconds (bad)
		// instead of        2^0, 2^1, 2^2, ..., 2^n seconds (good)
		nextInterval := r.NextInterval()

		r.MarkAttempt()

		// If the last callback called r.Break(), or if we've hit our call limit, bail out and return the last error we got (or nil)
		if r.ShouldGiveUp() {
			return err
		}

		r.sleepFunc(nextInterval)
	}
}
