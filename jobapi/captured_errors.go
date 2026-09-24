package jobapi

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/socket"
)

// MaxCapturedErrorBody is the local request limit in bytes, before the parent
// adds a timestamp and idempotency key.
const MaxCapturedErrorBody = 32 << 10

// UnmarshalJSON distinguishes omitted context from explicit null and preserves
// JSON numbers on both the CLI and parent sides of the local transport.
func (e *CapturedError) UnmarshalJSON(data []byte) error {
	type capturedError CapturedError
	var wire struct {
		capturedError
		Context json.RawMessage `json:"context"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return err
	}
	if len(wire.Context) != 0 {
		if bytes.Equal(bytes.TrimSpace(wire.Context), []byte("null")) {
			return errors.New("context must be an object")
		}
		dec := json.NewDecoder(bytes.NewReader(wire.Context))
		dec.UseNumber()
		if err := dec.Decode(&wire.capturedError.Context); err != nil {
			return err
		}
	}
	*e = CapturedError(wire.capturedError)
	return nil
}

func (s *Server) handleCapturedError(w http.ResponseWriter, r *http.Request) {
	if s.reportCapturedError == nil {
		s.writeCapturedError(w, errors.New("error capture is unavailable: enable the capture-error experiment on the parent agent"), http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxCapturedErrorBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeCapturedError(w, fmt.Errorf("captured error request exceeds %d bytes", MaxCapturedErrorBody), http.StatusRequestEntityTooLarge)
			return
		}
		s.writeCapturedError(w, fmt.Errorf("failed to read request body: %w", err), http.StatusBadRequest)
		return
	}
	payload := new(CapturedError)
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(payload); err != nil {
		s.writeCapturedError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest)
		return
	}
	if err := requireJSONEOF(dec); err != nil {
		s.writeCapturedError(w, err, http.StatusBadRequest)
		return
	}
	if err := validateCapturedError(payload); err != nil {
		s.writeCapturedError(w, err, http.StatusBadRequest)
		return
	}

	// Use the same registered values as job logs, including values added while
	// the job is running. Redact before both forwarding and the local response.
	if err := payload.redact(s.redactors.Needles()); err != nil {
		s.writeCapturedError(w, fmt.Errorf("redacting captured error: %w", err), http.StatusUnprocessableEntity)
		return
	}

	now := time.Now().UTC()
	if payload.Timestamp == nil {
		payload.Timestamp = &now
	} else {
		t := payload.Timestamp.UTC()
		payload.Timestamp = &t
	}
	payload.IdempotencyKey = rand.Text()

	if err := s.reportCapturedError(r.Context(), payload); err != nil {
		s.writeCapturedError(w, fmt.Errorf("reporting captured error: %w", err), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.Logger.Errorf("Job API: couldn't encode captured-error response: %v", err)
	}
}

func (e *CapturedError) redact(needles []string) error {
	if len(needles) == 0 {
		return nil
	}
	// These needles already include the escaped forms registered for logs.
	// Use a separate matcher so reports cannot flush or mix with log output.
	var output strings.Builder
	matcher := replacer.New(&output, needles, redact.Redacted)
	changed := false
	replace := func(s string) string {
		output.Reset()
		// Like redact.String, errors writing to a strings.Builder are bugs.
		if _, err := matcher.Write([]byte(s)); err != nil {
			panic(err)
		}
		if err := matcher.Flush(); err != nil {
			panic(err)
		}
		result := output.String()
		changed = changed || result != s
		return result
	}
	e.Code = replace(e.Code)
	e.Message = replace(e.Message)
	if e.Context != nil {
		context, err := redactCapturedErrorValue(e.Context, replace)
		if err != nil {
			return err
		}
		e.Context = context.(map[string]any)
	}
	if !changed {
		return nil
	}
	// Replacements can be longer than the original secret. Do not forward a
	// report that redaction made invalid or too large, or retry it unredacted.
	if err := validateCapturedError(e); err != nil {
		return err
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(body) > MaxCapturedErrorBody {
		return fmt.Errorf("captured error exceeds %d bytes after redaction", MaxCapturedErrorBody)
	}
	return nil
}

// Redact decoded data rather than JSON syntax: a secret can contain characters
// escaped by JSON, and replacing serialized text can break keys or numbers.
func redactCapturedErrorValue(value any, replace func(string) string) (any, error) {
	switch v := value.(type) {
	case string:
		return replace(v), nil
	case json.Number:
		if redacted := replace(v.String()); redacted != v.String() {
			// A numeric secret needs a string replacement to remain valid JSON.
			return redacted, nil
		}
	case []any:
		for i, item := range v {
			redacted, err := redactCapturedErrorValue(item, replace)
			if err != nil {
				return nil, err
			}
			v[i] = redacted
		}
	case map[string]any:
		redacted := make(map[string]any, len(v))
		for key, item := range v {
			key = replace(key)
			if _, exists := redacted[key]; exists {
				return nil, errors.New("context keys collide after redaction")
			}
			item, err := redactCapturedErrorValue(item, replace)
			if err != nil {
				return nil, err
			}
			redacted[key] = item
		}
		return redacted, nil
	}
	return value, nil
}

func requireJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON object")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func (s *Server) writeCapturedError(w http.ResponseWriter, err error, status int) {
	if writeErr := socket.WriteError(w, err, status); writeErr != nil {
		s.Logger.Errorf("Job API: couldn't write captured-error response: %v", writeErr)
	}
}

func validateCapturedError(e *CapturedError) error {
	if strings.TrimSpace(e.Code) == "" || len(e.Code) > 255 || strings.ContainsRune(e.Code, 0) {
		return errors.New("code must be a nonblank string of at most 255 bytes without NUL")
	}
	if strings.TrimSpace(e.Message) == "" || strings.ContainsRune(e.Message, 0) {
		return errors.New("message must be a nonblank string without NUL")
	}
	if e.IdempotencyKey != "" {
		return errors.New("idempotency_key is assigned by the parent agent and must be omitted")
	}
	if e.Timestamp != nil && (e.Timestamp.UTC().Year() < 1 || e.Timestamp.UTC().Year() > 9999) {
		return errors.New("timestamp must have a UTC year between 1 and 9999")
	}
	return nil
}
