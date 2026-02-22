package openapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestValidatorRejectsLargeRequestBody(t *testing.T) {
	spec := []byte(`
openapi: 3.0.3
info:
  title: Test API
  version: "1.0.0"
paths:
  /upload:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        "204":
          description: no content
`)

	called := false
	middleware := Validator(spec, Config{MaxRequestBodyBytes: 8})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(`{"name":"abcdef"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Fatalf("expected handler not to be called for oversized request body")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusRequestEntityTooLarge)
	}

	var payload ErrorPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response payload: %v", err)
	}
	if payload.Message != "Request body too large" {
		t.Fatalf("unexpected message: got=%q", payload.Message)
	}
}

func TestValidatorUsesResponseBodyForValidation(t *testing.T) {
	spec := []byte(`
openapi: 3.0.3
info:
  title: Test API
  version: "1.0.0"
paths:
  /test:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                required:
                  - name
                properties:
                  name:
                    type: string
`)

	capture := &captureSlogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(previous)

	middleware := Validator(spec, Config{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"ok"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusOK)
	}
	if capture.count("OpenAPI response validation error") != 0 {
		t.Fatalf("expected no response validation errors, got %d", capture.count("OpenAPI response validation error"))
	}
}

type captureSlogHandler struct {
	mu       sync.Mutex
	messages []string
}

func (h *captureSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureSlogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, r.Message)
	return nil
}

func (h *captureSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *captureSlogHandler) WithGroup(string) slog.Handler {
	return h
}

func (h *captureSlogHandler) count(message string) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	total := 0
	for _, msg := range h.messages {
		if msg == message {
			total++
		}
	}
	return total
}
