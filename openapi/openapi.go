package openapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	validationErrors "github.com/pb33f/libopenapi-validator/errors"
)

type ErrorDetail struct {
	Type   string `json:"type,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type ErrorPayload struct {
	Message string        `json:"message"`
	Errors  []ErrorDetail `json:"errors,omitempty"`
}

type ErrorEncoder interface {
	Encode(w http.ResponseWriter, statusCode int, payload ErrorPayload)
}

type ErrorEncoderFunc func(w http.ResponseWriter, statusCode int, payload ErrorPayload)

func (f ErrorEncoderFunc) Encode(w http.ResponseWriter, statusCode int, payload ErrorPayload) {
	f(w, statusCode, payload)
}

type Config struct {
	ErrorEncoder        ErrorEncoder
	MaxRequestBodyBytes int64
}

const DefaultMaxRequestBodyBytes int64 = 1 << 20

var errRequestBodyTooLarge = errors.New("request body too large")

func (c Config) withDefaults() Config {
	cfg := c
	if cfg.ErrorEncoder == nil {
		cfg.ErrorEncoder = ErrorEncoderFunc(defaultErrorEncoder)
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = DefaultMaxRequestBodyBytes
	}
	return cfg
}

func defaultErrorEncoder(w http.ResponseWriter, statusCode int, payload ErrorPayload) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// MountChi mounts oapi-codegen generated handlers onto a chi router.
func MountChi[T any](r chi.Router, handler T, register func(T, chi.Router) http.Handler, middlewares ...func(http.Handler) http.Handler) {
	if len(middlewares) == 0 {
		register(handler, r)
		return
	}

	r.Group(func(group chi.Router) {
		for _, mw := range middlewares {
			group.Use(mw)
		}
		register(handler, group)
	})
}

// Validator validates requests and responses against an OpenAPI spec.
func Validator(spec []byte, cfg Config) func(next http.Handler) http.Handler {
	conf := cfg.withDefaults()

	document, err := libopenapi.NewDocument(spec)
	if err != nil {
		panic(err)
	}

	v, errs := validator.NewValidator(document)
	if errs != nil {
		panic(errs)
	}

	if _, specErrs := v.ValidateDocument(); specErrs != nil {
		if len(specErrs) == 1 &&
			specErrs[0].ValidationType == "schema" &&
			len(specErrs[0].SchemaValidationErrors) == 1 &&
			specErrs[0].SchemaValidationErrors[0].Reason == "additional properties 'responses' not allowed" {
			slog.Debug("OpenAPI spec validated with known compatibility warning")
		} else {
			panic(fmt.Errorf("OpenAPI spec invalid: %v", specErrs))
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, err := readRequestBody(r.Body, conf.MaxRequestBodyBytes)
			if errors.Is(err, errRequestBodyTooLarge) {
				conf.ErrorEncoder.Encode(w, http.StatusRequestEntityTooLarge, ErrorPayload{Message: "Request body too large"})
				return
			}
			if err != nil {
				conf.ErrorEncoder.Encode(w, http.StatusBadRequest, ErrorPayload{Message: "Invalid request body"})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			valid, validationErrs := v.ValidateHttpRequest(r)
			if !valid {
				details := make([]ErrorDetail, 0, len(validationErrs))
				for _, validationErr := range validationErrs {
					reason := validationErr.Message
					if len(validationErr.SchemaValidationErrors) > 0 {
						reasons := mapSchemaValidationErrors(validationErr.SchemaValidationErrors)
						reason = strings.Join(reasons, "; ")
					}
					details = append(details, ErrorDetail{
						Type:   validationErr.ValidationType,
						Reason: reason,
					})
				}
				conf.ErrorEncoder.Encode(w, http.StatusBadRequest, ErrorPayload{
					Message: "Invalid request",
					Errors:  details,
				})
				return
			}

			rw := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			var bodyBuf bytes.Buffer
			rw.Tee(&bodyBuf)
			next.ServeHTTP(rw, r)
			statusCode := rw.Status()
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			resp := &http.Response{
				StatusCode: statusCode,
				Header:     rw.Header().Clone(),
				Body:       io.NopCloser(bytes.NewReader(bodyBuf.Bytes())),
			}
			valid, responseErrs := v.ValidateHttpResponse(r, resp)
			if !valid {
				for _, validationErr := range responseErrs {
					slog.Error("OpenAPI response validation error", "message", validationErr.Message)
				}
			}
		})
	}
}

func readRequestBody(body io.ReadCloser, limit int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()

	limitedReader := io.LimitReader(body, limit+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(bodyBytes)) > limit {
		return nil, errRequestBodyTooLarge
	}
	return bodyBytes, nil
}

func mapSchemaValidationErrors(errors []*validationErrors.SchemaValidationFailure) []string {
	result := make([]string, len(errors))
	for i, err := range errors {
		result[i] = err.Error()
	}
	return result
}
