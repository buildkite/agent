package plugin

import (
	"errors"
	"math/rand"
	"testing"
	"time"
)

func cyclicPermute[T any](arr []T) {
	if len(arr) < 2 {
		return
	}

	for i := len(arr) - 1; i > 0; i-- {
		j := rand.Intn(i)
		arr[i], arr[j] = arr[j], arr[i]
	}
}

func TestDeprecatedNameErrorsOrder(t *testing.T) {
	t.Parallel()
	seed := time.Now().UnixNano() % ((1 << 31) - 1)
	t.Logf("seed = %d", seed)
	rand.Seed(seed)

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
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			errs := make([]DeprecatedNameError, len(test.errs))
			copy(errs, test.errs)
			cyclicPermute(errs)

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
