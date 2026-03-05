package artifact

import (
	"time"

	"github.com/buildkite/agent/v3/api"
)

type retryIntervalSetter interface {
	SetNextInterval(time.Duration)
}

func applyRetryAfterHeader(resp *api.Response, r retryIntervalSetter) bool {
	if resp == nil {
		return false
	}

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return false
	}

	duration, err := time.ParseDuration(retryAfter + "s")
	if err != nil {
		return false
	}

	r.SetNextInterval(duration)
	return true
}
