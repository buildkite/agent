package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/buildkite/roko"
)

func TestHandleRetriableJobAcquisitionErrorLogsRetryAsString(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	retrier := roko.NewRetrier(roko.WithMaxAttempts(2))

	handleRetriableJobAcquisitionError(t.Context(), "retrying", errors.New("failed"), nil, retrier, logger)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", output.Bytes(), err)
	}
	if got, want := record["retry"], retrier.String(); got != want {
		t.Errorf("retry = %#v, want %q", got, want)
	}
}
