package retry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Insomniac implements a sleep function, but it doesn't actually sleep, it just notes down the intervals it was
// told to sleep
type insomniac struct {
	sleepIntervals []time.Duration
}

func newInsomniac() *insomniac {
	return &insomniac{sleepIntervals: []time.Duration{}}
}

func (i *insomniac) sleep(interval time.Duration) {
	i.sleepIntervals = append(i.sleepIntervals, interval)
}

func dummySleep(interval time.Duration) {}

var errDummy = errors.New("this makes it retry")

func TestDo(t *testing.T) {
	t.Parallel()

	i := newInsomniac()
	err := NewRetrier(
		WithStrategy(Exponential(2*time.Second, 0)),
		WithMaxAttempts(5),
		WithSleepFunc(i.sleep),
	).Do(func(_ *Retrier) error {
		return errDummy
	})

	assert.Error(t, err)

	assert.Equal(t,
		[]time.Duration{
			1 * time.Second,
			2 * time.Second,
			4 * time.Second,
			8 * time.Second,
			// There are only four waits, because after the fifth try (the fourth wait), the retrier gives up
		},
		i.sleepIntervals,
	)
}

func TestDo_OnSuccess_ReturnsNil(t *testing.T) {
	t.Parallel()

	callcount := 0
	i := newInsomniac()
	err := NewRetrier(
		WithStrategy(Exponential(2*time.Second, 0)),
		WithMaxAttempts(50),
		WithSleepFunc(i.sleep),
	).Do(func(_ *Retrier) error {
		callcount += 1
		if callcount >= 9 {
			// It "succeeded" on the 9th try
			return nil
		}
		return errDummy
	})

	assert.NoError(t, err)
	assert.Equal(t, 9, callcount)
}

func TestShouldGiveUp_WithMaxAttempts(t *testing.T) {
	t.Parallel()

	callcount := 0

	err := NewRetrier(
		WithStrategy(Constant(1*time.Second)),
		WithMaxAttempts(3),
		WithSleepFunc(dummySleep),
	).Do(func(_ *Retrier) error {
		callcount += 1
		return errDummy
	})

	assert.Error(t, err)
	assert.Equal(t, errDummy, err)

	assert.Equal(t, 3, callcount)
}

func TestShouldGiveUp_Break(t *testing.T) {
	t.Parallel()

	callcount := 0
	err := NewRetrier(
		WithStrategy(Constant(1*time.Second)),
		WithMaxAttempts(500),
		WithSleepFunc(dummySleep),
	).Do(func(r *Retrier) error {
		callcount += 1

		if callcount > 250 {
			r.Break()
		}

		return errDummy
	})

	assert.Error(t, err)
	assert.Equal(t, errDummy, err)

	assert.Less(t, callcount, 500) // ie, it broke before hitting max attampts
	assert.Equal(t, 251, callcount)
}

func TestShouldGiveUp_Forever(t *testing.T) {
	t.Parallel()

	err := NewRetrier(
		WithStrategy(Constant(1*time.Second)),
		TryForever(),
		WithSleepFunc(dummySleep),
	).Do(func(r *Retrier) error {
		assert.False(t, r.ShouldGiveUp())

		if r.AttemptCount() == 250_000 { // an arbitrarily large number of retries
			return nil
		}

		return errDummy
	})

	assert.NoError(t, err)
}

func TestNextInterval_ConstantStrategy(t *testing.T) {
	t.Parallel()

	insomniac := newInsomniac()
	err := NewRetrier(
		WithStrategy(Constant(5*time.Second)),
		WithMaxAttempts(1000),
		WithSleepFunc(insomniac.sleep),
	).Do(func(_ *Retrier) error { return errDummy })

	assert.Error(t, err)

	for _, interval := range insomniac.sleepIntervals {
		assert.Equal(t, interval, 5*time.Second)
	}
}

func TestNextInterval_ConstantStrategy_WithJitter(t *testing.T) {
	t.Parallel()

	expected := 5 * time.Second
	insomniac := newInsomniac()
	err := NewRetrier(
		WithStrategy(Constant(expected)),
		WithJitter(),
		WithMaxAttempts(1000),
		WithSleepFunc(insomniac.sleep),
	).Do(func(_ *Retrier) error { return errDummy })

	assert.Error(t, err)
	assert.Equal(t, errDummy, err)

	for _, interval := range insomniac.sleepIntervals {
		assert.Truef(t,
			withinJitterInterval(interval, expected),
			"actual interval %v was not within of expected interval %v", interval, jitterInterval, expected,
		)
	}
}

func TestNextInterval_ExponentialStrategy(t *testing.T) {
	t.Parallel()

	insomniac := newInsomniac()
	err := NewRetrier(
		WithStrategy(Exponential(2*time.Second, 0)),
		WithMaxAttempts(5),
		WithSleepFunc(insomniac.sleep),
	).Do(func(_ *Retrier) error { return errDummy })

	assert.Error(t, err)

	assert.Equal(t, insomniac.sleepIntervals, []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	})
}

func TestNextInterval_ExponentialStrategy_WithAdjustment(t *testing.T) {
	t.Parallel()

	insomniac := newInsomniac()
	err := NewRetrier(
		WithStrategy(Exponential(2*time.Second, 3*time.Second)),
		WithMaxAttempts(6),
		WithSleepFunc(insomniac.sleep),
	).Do(func(_ *Retrier) error { return errDummy })

	assert.Error(t, err)

	assert.Equal(t, insomniac.sleepIntervals, []time.Duration{
		4 * time.Second,
		5 * time.Second,
		7 * time.Second,
		11 * time.Second,
		19 * time.Second,
	})
}

func TestNextInterval_ExponentialStrategy_WithJitter(t *testing.T) {
	t.Parallel()

	insomniac := newInsomniac()
	err := NewRetrier(
		WithStrategy(Exponential(2*time.Second, 0)),
		WithMaxAttempts(6),
		WithSleepFunc(insomniac.sleep),
	).Do(func(_ *Retrier) error { return errDummy })

	assert.Error(t, err)

	expectedIntervals := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}

	for idx, actualInterval := range insomniac.sleepIntervals {
		assert.Truef(
			t,
			withinJitterInterval(actualInterval, expectedIntervals[idx]),
			"actual interval %v wasn't within 1s of expected interval %v", actualInterval, expectedIntervals[idx],
		)
	}
}

func withinJitterInterval(this, that time.Duration) bool {
	bigger := this
	smaller := that

	if bigger < smaller {
		bigger, smaller = smaller, bigger
	}

	diff := bigger - smaller

	return diff <= jitterInterval
}
