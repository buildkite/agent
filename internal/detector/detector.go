package detector

import (
	"errors"
	"fmt"
	"io"

	"github.com/buildkite/agent/v3/internal/replacer"
)

type Detector struct {
	detectee string
	detected bool
}

func New(dst io.Writer, detectee string) (io.Writer, *Detector) {
	d := &Detector{
		detected: false,
		detectee: detectee,
	}
	return replacer.New(
		dst,
		[]string{d.detectee},
		func(b []byte) []byte {
			d.detected = true
			return b
		},
	), d
}

func (d *Detector) Detected() bool {
	return d.detected
}

type DetectedErr struct {
	Detectee string
	inner    error
}

func NewDetectedErr(detectee string, err error) *DetectedErr {
	return &DetectedErr{
		Detectee: detectee,
		inner:    err,
	}
}

func (e *DetectedErr) Error() string {
	return fmt.Sprintf("error running command: %v, detected: %s", e.inner, e.Detectee)
}

func (e *DetectedErr) Unwrap() error {
	return e.inner
}

func (e *DetectedErr) Is(target error) bool {
	terr, ok := target.(*DetectedErr)
	// the detected slices were sorted on the way in, so we can compare them directly
	return ok && e.Detectee == terr.Detectee && errors.Is(e.inner, terr.inner)
}
