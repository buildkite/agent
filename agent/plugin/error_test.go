package plugin

import (
	"errors"
	"math/bits"
	"math/rand/v2"
	"testing"
	"time"
)

func shuffle[T any](rnd *rand.Rand, arr []T) {
	if len(arr) < 2 {
		return
	}
	rnd.Shuffle(len(arr), func(i, j int) {
		arr[i], arr[j] = arr[j], arr[i]
	})
}

func TestDeprecatedNameErrorsOrder(t *testing.T) {
	t.Parallel()

	seed1 := uint64(time.Now().UnixNano())
	seed2 := bits.Reverse64(uint64(time.Now().UnixNano()))
	t.Logf("seed1 = %d, seed2 = %d", seed1, seed2)

	for _, test := range []struct {
		name string
		errs []DeprecatedNameError
	}{
		{
			name: "0_error",
			errs: []DeprecatedNameError{},
		},
		{
			name: "1_error",
			errs: []DeprecatedNameError{
				{
					old: "a",
					new: "b",
				},
			},
		},
		{
			name: "2_errors",
			errs: []DeprecatedNameError{
				{
					old: "a",
					new: "b",
				},
				{
					old: "c",
					new: "d",
				},
			},
		},
		{
			name: "3_errors",
			errs: []DeprecatedNameError{
				{
					old: "a",
					new: "b",
				},
				{
					old: "c",
					new: "d",
				},
				{
					old: "e",
					new: "f",
				},
			},
		},
		{
			name: "4_errors",
			errs: []DeprecatedNameError{
				{
					old: "a",
					new: "b",
				},
				{
					old: "c",
					new: "d",
				},
				{
					old: "e",
					new: "f",
				},
				{
					old: "g",
					new: "h",
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			randSrc := rand.New(rand.NewPCG(seed1, seed2))
			errs := make([]DeprecatedNameError, len(test.errs))
			copy(errs, test.errs)
			shuffle(randSrc, errs)

			var err1, err2 *DeprecatedNameErrors
			err1 = err1.Append(test.errs...)
			err2 = err2.Append(errs...)
			if err1.Error() != err2.Error() {
				t.Errorf("expected DeprecatedNameErrors Error() to be ordered")
			}

			if !errors.Is(err1, err2) {
				t.Errorf("expected DeprecatedNameErrors Is() to not be sensitive to order")
			}
		})
	}
}
