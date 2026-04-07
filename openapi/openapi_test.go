package openapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
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

func TestMountChiWithoutMiddlewares(t *testing.T) {
	r := chi.NewRouter()
	handler := &mockHandler{}
	register := func(h *mockHandler, r chi.Router) http.Handler {
		r.Get("/test", h.ServeHTTP)
		return h
	}

	MountChi(r, handler, register)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if !handler.called {
		t.Fatal("expected handler to be called")
	}
}

func TestMountChiWithMiddlewares(t *testing.T) {
	r := chi.NewRouter()
	handler := &mockHandler{}
	var middlewareCalled bool
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, r)
		})
	}
	register := func(h *mockHandler, r chi.Router) http.Handler {
		r.Get("/test", h.ServeHTTP)
		return h
	}

	MountChi(r, handler, register, mw)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if !handler.called {
		t.Fatal("expected handler to be called")
	}
	if !middlewareCalled {
		t.Fatal("expected middleware to be called")
	}
}

type mockHandler struct {
	called bool
}

func (h *mockHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

func TestValidatorWithNilBody(t *testing.T) {
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
`)

	middleware := Validator(spec, Config{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Body is nil by default for GET requests
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestValidatorWithInvalidRequest(t *testing.T) {
	spec := []byte(`
openapi: 3.0.3
info:
  title: Test API
  version: "1.0.0"
paths:
  /test:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required:
                - name
              properties:
                name:
                  type: string
      responses:
        "200":
          description: ok
`)

	middleware := Validator(spec, Config{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send request without required "name" field
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"age":30}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusBadRequest)
	}

	var payload ErrorPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response payload: %v", err)
	}
	if payload.Message != "Invalid request" {
		t.Fatalf("unexpected message: got=%q", payload.Message)
	}
	if len(payload.Errors) == 0 {
		t.Fatal("expected validation errors")
	}
}

func TestValidatorWithResponseValidationError(t *testing.T) {
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
		// Missing required "name" field - should trigger response validation error
		_, _ = w.Write([]byte(`{"age":30}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Response still returns 200, but validation error is logged
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusOK)
	}
	if capture.count("OpenAPI response validation error") == 0 {
		t.Fatal("expected response validation error to be logged")
	}
}

func TestValidatorWithDefaults(t *testing.T) {
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
`)

	// Test with empty Config - should use defaults
	middleware := Validator(spec, Config{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestErrorEncoderFunc(t *testing.T) {
	var called bool
	f := ErrorEncoderFunc(func(w http.ResponseWriter, statusCode int, payload ErrorPayload) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(payload)
	})

	rr := httptest.NewRecorder()
	f.Encode(rr, http.StatusBadRequest, ErrorPayload{Message: "test error"})

	if !called {
		t.Fatal("expected ErrorEncoderFunc to be called")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusBadRequest)
	}
}

func TestValidatorWithCustomErrorEncoder(t *testing.T) {
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

	var customEncoderCalled bool
	customEncoder := ErrorEncoderFunc(func(w http.ResponseWriter, statusCode int, payload ErrorPayload) {
		customEncoderCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(payload)
	})

	middleware := Validator(spec, Config{
		ErrorEncoder:        customEncoder,
		MaxRequestBodyBytes: 8,
	})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(`{"name":"abcdefgh"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !customEncoderCalled {
		t.Fatal("expected custom error encoder to be called")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestValidatorPanicsOnInvalidSpec(t *testing.T) {
	invalidSpec := []byte(`not a valid yaml: [unterminated`)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Validator to panic on invalid spec")
		}
	}()

	_ = Validator(invalidSpec, Config{})
}

func TestValidatorPanicsOnSpecValidationFailure(t *testing.T) {
	// Spec with invalid structure that fails document validation
	invalidSpec := []byte(`
openapi: 3.0.3
info:
  title: Test API
  version: "1.0.0"
paths:
  /test:
    get:
      responses:
        invalid: response
`)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Validator to panic on spec validation failure")
		}
	}()

	_ = Validator(invalidSpec, Config{})
}

func TestValidatorWithKnownCompatibilityWarning(t *testing.T) {
	// Spec that triggers the known compatibility warning about 'responses' in responses
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
                properties:
                  name:
                    type: string
`)

	// This should not panic even if there's a known compatibility warning
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected Validator not to panic with valid spec, but got: %v", r)
		}
	}()

	middleware := Validator(spec, Config{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestReadRequestBodyWithNilBody(t *testing.T) {
	data, err := readRequestBody(nil, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data, got: %v", data)
	}
}

func TestReadRequestBodyWithExactLimit(t *testing.T) {
	body := io.NopCloser(strings.NewReader("hello"))
	data, err := readRequestBody(body, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected data: got=%q want=%q", string(data), "hello")
	}
}

func TestReadRequestBodyExceedsLimit(t *testing.T) {
	body := io.NopCloser(strings.NewReader("hello world"))
	_, err := readRequestBody(body, 5)
	if err != errRequestBodyTooLarge {
		t.Fatalf("unexpected error: got=%v want=%v", err, errRequestBodyTooLarge)
	}
}
