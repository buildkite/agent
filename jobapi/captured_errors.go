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
	"unicode/utf8"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/socket"
)

// MaxCapturedErrorBody is the local request limit in bytes, before the parent
// adds a timestamp and idempotency key.
const MaxCapturedErrorBody = 32 << 10

const maxCapturedErrorMessageLength = 1000

func (s *Server) handleCapturedError(w http.ResponseWriter, r *http.Request) {
	if s.reportCapturedError == nil {
		s.writeCapturedError(w, errors.New("error capture is unavailable on the parent agent"), http.StatusNotFound)
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

	// Redact the whole message first, so cutting through a secret cannot leave
	// an unrecognizable fragment of it in the report.
	var warning string
	if utf8.RuneCountInString(payload.Message) > maxCapturedErrorMessageLength {
		const marker = "…[truncated]"
		payload.Message = string([]rune(payload.Message)[:maxCapturedErrorMessageLength-utf8.RuneCountInString(marker)]) + marker
		warning = "Captured error message was truncated to 1000 characters. Put detailed output in an annotation or artifact."
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

	if warning != "" {
		s.Logger.Warningf("%s", warning)
	}
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(CapturedErrorResponse{CapturedError: *payload, Warning: warning}); err != nil {
		s.Logger.Errorf("Job API: couldn't encode captured-error response: %v", err)
	}
}

func (e *CapturedError) redact(needles []string) error {
	// These needles already include the escaped forms registered for logs.
	// Use a separate matcher so reports cannot flush or mix with log output.
	// Like job logs, also redact anything that looks like a Buildkite-issued
	// token, even when its value was never registered.
	var output strings.Builder
	matcher := replacer.New(&output, needles, redact.Redacted)
	matcher.AddPrefixes(redact.TokenPrefixes()...)
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
	if !changed {
		return nil
	}
	// Redaction can expand the code beyond its limit. Reject an invalid code;
	// the caller will shorten an overlong message after redaction.
	return validateCapturedError(e)
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
